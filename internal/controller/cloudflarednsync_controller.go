// Package controller contains the reconciliation logic for cfgate CRDs.
package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	cfgatev1alpha1 "cfgate.io/cfgate/api/v1alpha1"
	"cfgate.io/cfgate/internal/cloudflare"
)

const (
	// dnsSyncFinalizer is the finalizer for CloudflareDNSSync resources.
	dnsSyncFinalizer = "cfgate.io/dns-cleanup"

	// ConditionTypeZonesResolved indicates all configured zones were resolved.
	ConditionTypeZonesResolved = "ZonesResolved"

	// ConditionTypeDNSSynced indicates DNS records are synced.
	ConditionTypeDNSSynced = "DNSSynced"

	// defaultOwnershipPrefix is the default prefix for TXT ownership records.
	defaultOwnershipPrefix = "_cfgate"
)

// CloudflareDNSSyncReconciler reconciles a CloudflareDNSSync object.
// It manages DNS records for CloudflareTunnel resources by watching
// Gateway API routes and syncing hostnames to Cloudflare DNS.
type CloudflareDNSSyncReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder

	// CFClient is the Cloudflare API client. Injected for testing.
	CFClient cloudflare.Client

	// CredentialCache caches validated Cloudflare clients to avoid repeated validations.
	CredentialCache *cloudflare.CredentialCache
}

// +kubebuilder:rbac:groups=cfgate.io,resources=cloudflarednssyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cfgate.io,resources=cloudflarednssyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cfgate.io,resources=cloudflarednssyncs/finalizers,verbs=update

// Reconcile handles the reconciliation loop for CloudflareDNSSync resources.
// It collects hostnames from routes, resolves zones, and syncs DNS records.
func (r *CloudflareDNSSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling CloudflareDNSSync", "name", req.Name, "namespace", req.Namespace)

	// 1. Fetch CloudflareDNSSync resource
	var sync cfgatev1alpha1.CloudflareDNSSync
	if err := r.Get(ctx, req.NamespacedName, &sync); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CloudflareDNSSync not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get CloudflareDNSSync: %w", err)
	}

	// 2. Handle deletion
	if !sync.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &sync)
	}

	// Add finalizer if not present (using patch to reduce lock contention)
	if !controllerutil.ContainsFinalizer(&sync, dnsSyncFinalizer) {
		patch := client.MergeFrom(sync.DeepCopy())
		controllerutil.AddFinalizer(&sync, dnsSyncFinalizer)
		if err := r.Patch(ctx, &sync, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 3. Resolve referenced tunnel
	tunnel, err := r.resolveTunnel(ctx, &sync)
	if err != nil {
		log.Error(err, "failed to resolve tunnel")
		r.setCondition(&sync, ConditionTypeReady, metav1.ConditionFalse, "TunnelNotFound", err.Error())
		if err := r.updateStatus(ctx, &sync); err != nil {
			log.Error(err, "failed to update status")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if tunnel.Status.TunnelID == "" {
		log.Info("Tunnel not ready yet", "tunnel", tunnel.Name)
		r.setCondition(&sync, ConditionTypeReady, metav1.ConditionFalse, "TunnelNotReady", "Referenced tunnel is not ready")
		if err := r.updateStatus(ctx, &sync); err != nil {
			log.Error(err, "failed to update status")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// 4. Collect hostnames from routes
	hostnames, err := r.collectHostnames(ctx, &sync, tunnel)
	if err != nil {
		log.Error(err, "failed to collect hostnames")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 5. Resolve zones
	zones, err := r.resolveZones(ctx, &sync, tunnel)
	if err != nil {
		log.Error(err, "failed to resolve zones")
		r.setCondition(&sync, ConditionTypeZonesResolved, metav1.ConditionFalse, "ZoneResolutionFailed", err.Error())
		if err := r.updateStatus(ctx, &sync); err != nil {
			log.Error(err, "failed to update status")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	r.setCondition(&sync, ConditionTypeZonesResolved, metav1.ConditionTrue, "ZonesResolved", "All zones resolved successfully")

	// 6. Sync records
	if err := r.syncRecords(ctx, &sync, tunnel, hostnames, zones); err != nil {
		log.Error(err, "failed to sync records")
		r.setCondition(&sync, ConditionTypeDNSSynced, metav1.ConditionFalse, "SyncFailed", err.Error())
		if err := r.updateStatus(ctx, &sync); err != nil {
			log.Error(err, "failed to update status")
		}
		r.Recorder.Eventf(&sync, nil, corev1.EventTypeWarning, "SyncFailed", "Sync", "%s", err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	r.setCondition(&sync, ConditionTypeDNSSynced, metav1.ConditionTrue, "RecordsSynced", "DNS records synced successfully")

	// 7. Update status
	r.setCondition(&sync, ConditionTypeReady, metav1.ConditionTrue, "Ready", "DNS sync is operational")
	sync.Status.ObservedGeneration = sync.Generation
	now := metav1.Now()
	sync.Status.LastSyncTime = &now

	if err := r.updateStatus(ctx, &sync); err != nil {
		log.Error(err, "failed to update status")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.Recorder.Eventf(&sync, nil, corev1.EventTypeNormal, "Reconciled", "Reconcile", "DNS sync completed successfully")
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
// Uses GenerationChangedPredicate to avoid reconciling on status-only updates,
// which prevents the 1-second reconciliation loop caused by status updates.
func (r *CloudflareDNSSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfgatev1alpha1.CloudflareDNSSync{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&gateway.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(r.findAffectedDNSSyncs),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Watches(
			&gateway.Gateway{},
			handler.EnqueueRequestsFromMapFunc(r.findAffectedDNSSyncs),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Complete(r)
}

// findAffectedDNSSyncs finds all CloudflareDNSSync resources that may be affected
// by a change to an HTTPRoute or Gateway. This enables reactive reconciliation
// when routes are created, updated, or deleted.
func (r *CloudflareDNSSyncReconciler) findAffectedDNSSyncs(ctx context.Context, obj client.Object) []reconcile.Request {
	log := log.FromContext(ctx)

	// List all CloudflareDNSSync resources
	var syncList cfgatev1alpha1.CloudflareDNSSyncList
	if err := r.List(ctx, &syncList); err != nil {
		log.Error(err, "failed to list CloudflareDNSSync resources")
		return nil
	}

	// For simplicity, trigger reconciliation for all DNSSync resources
	// that have gateway routes enabled. The reconciler will filter appropriately.
	var requests []reconcile.Request
	for _, sync := range syncList.Items {
		if sync.Spec.Source.GatewayRoutes.Enabled {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      sync.Name,
					Namespace: sync.Namespace,
				},
			})
		}
	}

	if len(requests) > 0 {
		log.Info("HTTPRoute/Gateway change triggering DNSSync reconciliation",
			"object", obj.GetName(),
			"objectKind", obj.GetObjectKind().GroupVersionKind().Kind,
			"affectedDNSSyncs", len(requests))
	}

	return requests
}

// resolveTunnel resolves the referenced CloudflareTunnel.
func (r *CloudflareDNSSyncReconciler) resolveTunnel(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync) (*cfgatev1alpha1.CloudflareTunnel, error) {
	namespace := sync.Spec.TunnelRef.Namespace
	if namespace == "" {
		namespace = sync.Namespace
	}

	var tunnel cfgatev1alpha1.CloudflareTunnel
	if err := r.Get(ctx, types.NamespacedName{
		Name:      sync.Spec.TunnelRef.Name,
		Namespace: namespace,
	}, &tunnel); err != nil {
		return nil, fmt.Errorf("failed to get tunnel %s/%s: %w", namespace, sync.Spec.TunnelRef.Name, err)
	}

	return &tunnel, nil
}

// collectHostnames collects hostnames from Gateway API routes.
// Returns a list of hostnames to sync based on the source configuration.
func (r *CloudflareDNSSyncReconciler) collectHostnames(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync, tunnel *cfgatev1alpha1.CloudflareTunnel) ([]string, error) {
	var hostnames []string

	// Collect from explicit hostnames
	for _, explicit := range sync.Spec.Source.Explicit {
		hostnames = append(hostnames, explicit.Hostname)
	}

	// Collect from Gateway routes if enabled
	if sync.Spec.Source.GatewayRoutes.Enabled {
		routeHostnames, err := r.collectHostnamesFromRoutes(ctx, sync, tunnel)
		if err != nil {
			return nil, err
		}
		hostnames = append(hostnames, routeHostnames...)
	}

	// Deduplicate
	seen := make(map[string]bool)
	var unique []string
	for _, h := range hostnames {
		if !seen[h] {
			seen[h] = true
			unique = append(unique, h)
		}
	}

	return unique, nil
}

// collectHostnamesFromRoutes collects hostnames from HTTPRoutes.
func (r *CloudflareDNSSyncReconciler) collectHostnamesFromRoutes(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync, tunnel *cfgatev1alpha1.CloudflareTunnel) ([]string, error) {
	var hostnames []string

	// Find Gateways that reference this tunnel
	var gateways gateway.GatewayList
	if err := r.List(ctx, &gateways); err != nil {
		return nil, fmt.Errorf("failed to list gateways: %w", err)
	}

	tunnelRef := fmt.Sprintf("%s/%s", tunnel.Namespace, tunnel.Name)
	var relevantGateways []gateway.Gateway

	for _, gw := range gateways.Items {
		if ref, ok := gw.Annotations[AnnotationTunnelRef]; ok && ref == tunnelRef {
			// Check if DNS sync is enabled on gateway
			if dnsSync, ok := gw.Annotations[AnnotationDNSSync]; ok && dnsSync == "enabled" {
				relevantGateways = append(relevantGateways, gw)
			}
		}
	}

	// For each Gateway, find HTTPRoutes
	for _, gw := range relevantGateways {
		var routes gateway.HTTPRouteList
		if err := r.List(ctx, &routes); err != nil {
			return nil, fmt.Errorf("failed to list httproutes: %w", err)
		}

		for _, route := range routes.Items {
			// Check annotation filter if specified
			if sync.Spec.Source.GatewayRoutes.AnnotationFilter != "" {
				if _, ok := route.Annotations[sync.Spec.Source.GatewayRoutes.AnnotationFilter]; !ok {
					continue
				}
			}

			// Check if route references this gateway
			for _, parentRef := range route.Spec.ParentRefs {
				parentNS := route.Namespace
				if parentRef.Namespace != nil {
					parentNS = string(*parentRef.Namespace)
				}

				if string(parentRef.Name) == gw.Name && parentNS == gw.Namespace {
					// Collect hostnames from route
					for _, h := range route.Spec.Hostnames {
						hostnames = append(hostnames, string(h))
					}
				}
			}
		}
	}

	return hostnames, nil
}

// resolveZones resolves zone names to zone IDs.
// Uses cached IDs if provided, otherwise looks up via API.
func (r *CloudflareDNSSyncReconciler) resolveZones(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync, tunnel *cfgatev1alpha1.CloudflareTunnel) (map[string]string, error) {
	zones := make(map[string]string)

	cfClient, err := r.getCloudflareClient(ctx, tunnel)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloudflare client: %w", err)
	}

	dnsService := cloudflare.NewDNSService(cfClient)

	for _, zoneConfig := range sync.Spec.Zones {
		if zoneConfig.ID != "" {
			// Use cached ID
			zones[zoneConfig.Name] = zoneConfig.ID
		} else {
			// Look up zone
			zone, err := dnsService.ResolveZone(ctx, zoneConfig.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve zone %s: %w", zoneConfig.Name, err)
			}
			if zone == nil {
				return nil, fmt.Errorf("zone %s not found", zoneConfig.Name)
			}
			zones[zoneConfig.Name] = zone.ID
		}
	}

	return zones, nil
}

// syncRecords syncs DNS records to Cloudflare.
// Compares desired state with actual state and applies changes.
func (r *CloudflareDNSSyncReconciler) syncRecords(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync, tunnel *cfgatev1alpha1.CloudflareTunnel, hostnames []string, zones map[string]string) error {
	log := log.FromContext(ctx)

	cfClient, err := r.getCloudflareClient(ctx, tunnel)
	if err != nil {
		return fmt.Errorf("failed to create Cloudflare client: %w", err)
	}

	dnsService := cloudflare.NewDNSService(cfClient)
	tunnelDomain := tunnel.Status.TunnelDomain

	ownershipPrefix := sync.Spec.Ownership.TXTRecord.Prefix
	if ownershipPrefix == "" {
		ownershipPrefix = defaultOwnershipPrefix
	}

	var recordStatuses []cfgatev1alpha1.DNSRecordStatus
	var syncedCount, pendingCount, failedCount int32

	for _, hostname := range hostnames {
		// Determine zone for this hostname
		zoneName := cloudflare.ExtractZoneFromHostname(hostname)
		zoneID, ok := zones[zoneName]
		if !ok {
			log.Info("Zone not configured for hostname", "hostname", hostname, "zone", zoneName)
			recordStatuses = append(recordStatuses, cfgatev1alpha1.DNSRecordStatus{
				Hostname: hostname,
				Type:     "CNAME",
				Status:   "Failed",
				Error:    fmt.Sprintf("zone %s not configured", zoneName),
			})
			failedCount++
			continue
		}

		// Build desired record
		comment := fmt.Sprintf("managed by cfgate, tunnel=%s", tunnel.Name)
		desired := cloudflare.BuildCNAMERecord(hostname, tunnelDomain, sync.Spec.Defaults.Proxied, comment)

		// Sync record
		record, modified, err := dnsService.SyncRecord(ctx, zoneID, desired)
		if err != nil {
			log.Error(err, "failed to sync DNS record", "hostname", hostname)
			recordStatuses = append(recordStatuses, cfgatev1alpha1.DNSRecordStatus{
				Hostname: hostname,
				Type:     "CNAME",
				Status:   "Failed",
				Error:    err.Error(),
			})
			failedCount++
			continue
		}

		// Create ownership TXT record if enabled
		if sync.Spec.Ownership.TXTRecord.Enabled {
			if err := dnsService.CreateOwnershipRecord(ctx, zoneID, hostname, tunnel.Name, ownershipPrefix); err != nil {
				// Non-fatal: ownership records are supplementary, don't fail sync
				log.V(1).Info("ownership record sync issue", "hostname", hostname, "error", err.Error())
			}
		}

		status := "Synced"
		if modified {
			log.Info("DNS record modified", "hostname", hostname, "recordID", record.ID)
			r.Recorder.Eventf(sync, nil, corev1.EventTypeNormal, "RecordSynced", "Sync", "DNS record synced: %s", hostname)
		}

		recordStatuses = append(recordStatuses, cfgatev1alpha1.DNSRecordStatus{
			Hostname: hostname,
			Type:     record.Type,
			Target:   record.Content,
			Proxied:  record.Proxied,
			Status:   status,
			RecordID: record.ID,
		})
		syncedCount++
	}

	// Delete orphaned records (previously synced but no longer wanted)
	for _, prevRecord := range sync.Status.Records {
		found := false
		for _, hostname := range hostnames {
			if prevRecord.Hostname == hostname {
				found = true
				break
			}
		}
		if !found && prevRecord.RecordID != "" {
			// This record was previously synced but hostname is no longer wanted
			zoneName := cloudflare.ExtractZoneFromHostname(prevRecord.Hostname)
			zoneID, ok := zones[zoneName]
			if ok {
				// Check ownership before deleting
				existingRecord, err := dnsService.FindRecordByName(ctx, zoneID, prevRecord.Hostname, prevRecord.Type)
				if err == nil && existingRecord != nil && cloudflare.IsOwnedByCfgate(existingRecord, "", "") {
					if err := dnsService.DeleteRecord(ctx, zoneID, prevRecord.RecordID); err != nil {
						log.Error(err, "failed to delete orphaned DNS record", "hostname", prevRecord.Hostname)
					} else {
						log.Info("Deleted orphaned DNS record", "hostname", prevRecord.Hostname)
						r.Recorder.Eventf(sync, nil, corev1.EventTypeNormal, "RecordDeleted", "Delete", "DNS record deleted: %s", prevRecord.Hostname)
					}

					// Delete ownership TXT record if enabled
					if sync.Spec.Ownership.TXTRecord.Enabled {
						if err := dnsService.DeleteOwnershipRecord(ctx, zoneID, prevRecord.Hostname, ownershipPrefix); err != nil {
							log.Error(err, "failed to delete ownership record", "hostname", prevRecord.Hostname)
						}
					}
				}
			}
		}
	}

	// Update status
	sync.Status.Records = recordStatuses
	sync.Status.SyncedRecords = syncedCount
	sync.Status.PendingRecords = pendingCount
	sync.Status.FailedRecords = failedCount

	return nil
}

// reconcileDelete handles deletion of CloudflareDNSSync.
// Uses fallback credentials if the tunnel's credentials are unavailable.
func (r *CloudflareDNSSyncReconciler) reconcileDelete(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("handling DNSSync deletion", "name", sync.Name)

	if !controllerutil.ContainsFinalizer(sync, dnsSyncFinalizer) {
		return ctrl.Result{}, nil
	}

	// Cleanup records if policy allows
	if sync.Spec.CleanupPolicy.DeleteOnResourceRemoval {
		if err := r.cleanupRecordsWithFallback(ctx, sync); err != nil {
			log.Error(err, "failed to cleanup DNS records, records may be orphaned")
			r.Recorder.Eventf(sync, nil, corev1.EventTypeWarning, "DNSCleanupFailed", "Cleanup",
				"DNS cleanup failed, records may be orphaned: %v", err)
			// Continue with finalizer removal - don't block deletion
		}
	}

	// Remove finalizer using patch to reduce lock contention
	patch := client.MergeFrom(sync.DeepCopy())
	controllerutil.RemoveFinalizer(sync, dnsSyncFinalizer)
	if err := r.Patch(ctx, sync, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// updateStatus updates the CloudflareDNSSync status only if it has changed.
// This avoids unnecessary API calls and prevents watch events from status-only updates.
func (r *CloudflareDNSSyncReconciler) updateStatus(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync) error {
	// Re-fetch to avoid conflicts
	var current cfgatev1alpha1.CloudflareDNSSync
	if err := r.Get(ctx, types.NamespacedName{Name: sync.Name, Namespace: sync.Namespace}, &current); err != nil {
		return fmt.Errorf("failed to re-fetch DNSSync: %w", err)
	}

	// Check if status actually changed (excluding LastSyncTime which always changes)
	if statusEqual(&current.Status, &sync.Status) {
		return nil // No update needed
	}

	// Copy status
	current.Status = sync.Status

	if err := r.Status().Update(ctx, &current); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	return nil
}

// statusEqual compares two DNSSync statuses for equality.
// Ignores LastSyncTime as it changes on every reconciliation.
func statusEqual(a, b *cfgatev1alpha1.CloudflareDNSSyncStatus) bool {
	// Compare generation
	if a.ObservedGeneration != b.ObservedGeneration {
		return false
	}

	// Compare record counts
	if a.SyncedRecords != b.SyncedRecords ||
		a.PendingRecords != b.PendingRecords ||
		a.FailedRecords != b.FailedRecords {
		return false
	}

	// Compare conditions (ignoring LastTransitionTime)
	if len(a.Conditions) != len(b.Conditions) {
		return false
	}
	for i := range a.Conditions {
		if a.Conditions[i].Type != b.Conditions[i].Type ||
			a.Conditions[i].Status != b.Conditions[i].Status ||
			a.Conditions[i].Reason != b.Conditions[i].Reason ||
			a.Conditions[i].Message != b.Conditions[i].Message {
			return false
		}
	}

	// Compare records
	if !reflect.DeepEqual(a.Records, b.Records) {
		return false
	}

	return true
}

// getCloudflareClient creates or returns the Cloudflare client.
// Uses credential cache to avoid repeated API validations.
func (r *CloudflareDNSSyncReconciler) getCloudflareClient(ctx context.Context, tunnel *cfgatev1alpha1.CloudflareTunnel) (cloudflare.Client, error) {
	// If injected client exists, use it (for testing)
	if r.CFClient != nil {
		return r.CFClient, nil
	}

	// Get credentials from secret
	secretNamespace := tunnel.Spec.Cloudflare.SecretRef.Namespace
	if secretNamespace == "" {
		secretNamespace = tunnel.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      tunnel.Spec.Cloudflare.SecretRef.Name,
		Namespace: secretNamespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get credentials secret: %w", err)
	}

	// Use cache if available
	if r.CredentialCache != nil {
		return r.CredentialCache.GetOrCreate(ctx, secret, func() (cloudflare.Client, error) {
			return r.createClientFromSecret(secret, tunnel.Spec.Cloudflare.SecretKeys.APIToken)
		})
	}

	return r.createClientFromSecret(secret, tunnel.Spec.Cloudflare.SecretKeys.APIToken)
}

// createClientFromSecret creates a Cloudflare client from a secret.
func (r *CloudflareDNSSyncReconciler) createClientFromSecret(secret *corev1.Secret, tokenKey string) (cloudflare.Client, error) {
	if tokenKey == "" {
		tokenKey = "CLOUDFLARE_API_TOKEN"
	}

	token, ok := secret.Data[tokenKey]
	if !ok {
		return nil, fmt.Errorf("API token key %q not found in secret", tokenKey)
	}

	return cloudflare.NewClient(string(token))
}

// getCloudflareClientWithFallback tries tunnel credentials, then fallback credentials.
// Used during deletion when the tunnel or its secret may have been deleted.
func (r *CloudflareDNSSyncReconciler) getCloudflareClientWithFallback(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync) (cloudflare.Client, error) {
	log := log.FromContext(ctx)

	// Try tunnel credentials first
	tunnel, err := r.resolveTunnel(ctx, sync)
	if err == nil {
		cfClient, err := r.getCloudflareClient(ctx, tunnel)
		if err == nil {
			return cfClient, nil
		}
		log.V(1).Info("tunnel credentials unavailable", "error", err)
	} else {
		log.V(1).Info("tunnel not found", "error", err)
	}

	// Check if we have fallback credentials
	if sync.Spec.FallbackCredentialsRef == nil {
		return nil, fmt.Errorf("tunnel credentials unavailable and no fallback configured")
	}

	log.Info("using fallback credentials for DNS cleanup",
		"fallbackSecret", sync.Spec.FallbackCredentialsRef.Name,
		"fallbackNamespace", sync.Spec.FallbackCredentialsRef.Namespace)

	// Try fallback credentials
	fallbackNamespace := sync.Spec.FallbackCredentialsRef.Namespace
	if fallbackNamespace == "" {
		fallbackNamespace = sync.Namespace
	}

	fallbackSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      sync.Spec.FallbackCredentialsRef.Name,
		Namespace: fallbackNamespace,
	}, fallbackSecret); err != nil {
		return nil, fmt.Errorf("failed to get fallback credentials secret: %w", err)
	}

	token, ok := fallbackSecret.Data["CLOUDFLARE_API_TOKEN"]
	if !ok {
		return nil, fmt.Errorf("CLOUDFLARE_API_TOKEN not found in fallback secret")
	}

	return cloudflare.NewClient(string(token))
}

// cleanupRecordsWithFallback deletes managed DNS records using fallback credentials if needed.
func (r *CloudflareDNSSyncReconciler) cleanupRecordsWithFallback(ctx context.Context, sync *cfgatev1alpha1.CloudflareDNSSync) error {
	log := log.FromContext(ctx)

	// Get Cloudflare client (with fallback)
	cfClient, err := r.getCloudflareClientWithFallback(ctx, sync)
	if err != nil {
		return fmt.Errorf("failed to get Cloudflare client: %w", err)
	}

	dnsService := cloudflare.NewDNSService(cfClient)

	ownershipPrefix := sync.Spec.Ownership.TXTRecord.Prefix
	if ownershipPrefix == "" {
		ownershipPrefix = defaultOwnershipPrefix
	}

	// Get tunnel name for ownership check (may not be available)
	var tunnelName string
	tunnel, err := r.resolveTunnel(ctx, sync)
	if err == nil {
		tunnelName = tunnel.Name
	}

	// For each zone, find and delete managed records
	for _, zoneConfig := range sync.Spec.Zones {
		zoneID := zoneConfig.ID
		if zoneID == "" {
			zone, err := dnsService.ResolveZone(ctx, zoneConfig.Name)
			if err != nil || zone == nil {
				log.Error(err, "failed to resolve zone for cleanup", "zone", zoneConfig.Name)
				continue
			}
			zoneID = zone.ID
		}

		// List managed records
		records, err := dnsService.ListManagedRecords(ctx, zoneID, ownershipPrefix)
		if err != nil {
			log.Error(err, "failed to list managed records", "zone", zoneConfig.Name)
			continue
		}

		for _, record := range records {
			if cloudflare.IsOwnedByCfgate(&record, "", tunnelName) || !sync.Spec.CleanupPolicy.OnlyManaged {
				if err := dnsService.DeleteRecord(ctx, zoneID, record.ID); err != nil {
					log.Error(err, "failed to delete DNS record", "record", record.Name)
				} else {
					log.Info("Deleted DNS record", "record", record.Name)
				}
			}
		}
	}

	return nil
}

// setCondition sets a condition on the DNSSync status.
func (r *CloudflareDNSSyncReconciler) setCondition(sync *cfgatev1alpha1.CloudflareDNSSync, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: sync.Generation,
	}

	meta.SetStatusCondition(&sync.Status.Conditions, condition)
}
