// Package context provides wrapper types for clean separation between raw API
// types and processing logic. Based on Envoy Gateway's GatewayContext and
// ListenerContext patterns.
package context

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwapiv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwapiv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	cfgatev1alpha1 "cfgate.io/cfgate/api/v1alpha1"
	"cfgate.io/cfgate/internal/cloudflare"
)

// -----------------------------------------------------------------------------
// TunnelContext
// -----------------------------------------------------------------------------

// TunnelContext wraps CloudflareTunnel with computed state and helper methods.
// It provides a clean interface for reconcilers to work with tunnels without
// repeatedly computing derived values.
type TunnelContext struct {
	// Embedded tunnel resource
	*cfgatev1alpha1.CloudflareTunnel

	// Computed fields (populated by NewTunnelContext)
	resolvedAccountID string
	tunnelClient      cloudflare.Client
	dnsClient         *cloudflare.DNSService

	// Logger for this context
	log logr.Logger
}

// NewTunnelContext creates a TunnelContext with resolved clients.
// Returns error if credentials cannot be resolved.
func NewTunnelContext(
	tunnel *cfgatev1alpha1.CloudflareTunnel,
	accountID string,
	tunnelClient cloudflare.Client,
	dnsClient *cloudflare.DNSService,
) *TunnelContext {
	log := ctrl.Log.WithName("context").WithName("tunnel").
		WithValues("tunnel", tunnel.Namespace+"/"+tunnel.Name)

	return &TunnelContext{
		CloudflareTunnel:  tunnel,
		resolvedAccountID: accountID,
		tunnelClient:      tunnelClient,
		dnsClient:         dnsClient,
		log:               log,
	}
}

// HasDNSEnabled returns true if DNS sync is enabled for this tunnel.
func (tc *TunnelContext) HasDNSEnabled() bool {
	return tc.Spec.DNS != nil && tc.Spec.DNS.Enabled
}

// GetZones returns configured DNS zones (empty if DNS disabled).
func (tc *TunnelContext) GetZones() []cfgatev1alpha1.TunnelZoneConfig {
	if tc.Spec.DNS == nil {
		return nil
	}
	return tc.Spec.DNS.Zones
}

// GetOwnershipPrefix returns the TXT ownership record prefix.
// Defaults to "_cfgate" if not configured.
func (tc *TunnelContext) GetOwnershipPrefix() string {
	if tc.Spec.DNS == nil ||
		tc.Spec.DNS.Ownership == nil ||
		tc.Spec.DNS.Ownership.TXTRecord == nil ||
		tc.Spec.DNS.Ownership.TXTRecord.Prefix == "" {
		return "_cfgate"
	}
	return tc.Spec.DNS.Ownership.TXTRecord.Prefix
}

// GetOwnerIdentifier returns the unique owner identifier for TXT records.
// Format: <namespace>/<name>
func (tc *TunnelContext) GetOwnerIdentifier() string {
	return tc.Namespace + "/" + tc.Name
}

// GetDNSPolicy returns the configured DNS policy (defaults to "sync").
func (tc *TunnelContext) GetDNSPolicy() cfgatev1alpha1.DNSPolicy {
	if tc.Spec.DNS == nil || tc.Spec.DNS.Policy == "" {
		return cfgatev1alpha1.DNSPolicySync
	}
	return tc.Spec.DNS.Policy
}

// ShouldCreateTXTRecords returns true if TXT ownership records should be created.
func (tc *TunnelContext) ShouldCreateTXTRecords() bool {
	if tc.Spec.DNS == nil ||
		tc.Spec.DNS.Ownership == nil ||
		tc.Spec.DNS.Ownership.TXTRecord == nil {
		return true // default enabled
	}
	return tc.Spec.DNS.Ownership.TXTRecord.Enabled
}

// ShouldDeleteOnRemoval returns true if DNS records should be deleted
// when source resources are removed.
func (tc *TunnelContext) ShouldDeleteOnRemoval() bool {
	if tc.Spec.DNS == nil ||
		tc.Spec.DNS.CleanupPolicy == nil {
		return true // default enabled
	}
	return tc.Spec.DNS.CleanupPolicy.DeleteOnResourceRemoval
}

// AccountID returns the resolved Cloudflare account ID.
func (tc *TunnelContext) AccountID() string {
	return tc.resolvedAccountID
}

// TunnelClient returns the tunnel API client.
func (tc *TunnelContext) TunnelClient() cloudflare.Client {
	return tc.tunnelClient
}

// DNSClient returns the DNS API client (nil if DNS disabled).
func (tc *TunnelContext) DNSClient() *cloudflare.DNSService {
	return tc.dnsClient
}

// -----------------------------------------------------------------------------
// AccessPolicyContext
// -----------------------------------------------------------------------------

// AccessPolicyContext wraps CloudflareAccessPolicy with resolved targets and
// helper methods for reconciliation.
type AccessPolicyContext struct {
	// Embedded policy resource
	*cfgatev1alpha1.CloudflareAccessPolicy

	// Computed fields (populated by NewAccessPolicyContext)
	resolvedTargets []TargetInfo

	// Logger for this context
	log logr.Logger
}

// NewAccessPolicyContext creates an AccessPolicyContext with resolved targets.
// Target resolution errors are captured in TargetInfo.Error (partial resolution).
func NewAccessPolicyContext(
	ctx context.Context,
	policy *cfgatev1alpha1.CloudflareAccessPolicy,
	k8sClient client.Client,
) *AccessPolicyContext {
	log := ctrl.Log.WithName("context").WithName("accesspolicy").
		WithValues("policy", policy.Namespace+"/"+policy.Name)

	// Resolve targets
	targets := resolveTargets(ctx, policy, k8sClient, log)
	log.V(1).Info("resolved targets",
		"total", len(targets),
		"resolved", countResolved(targets),
		"failed", countFailed(targets),
	)

	return &AccessPolicyContext{
		CloudflareAccessPolicy: policy,
		resolvedTargets:        targets,
		log:                    log,
	}
}

// GetTargetRefs returns all target references (merged from targetRef and targetRefs).
func (apc *AccessPolicyContext) GetTargetRefs() []cfgatev1alpha1.PolicyTargetReference {
	var refs []cfgatev1alpha1.PolicyTargetReference

	if apc.Spec.TargetRef != nil {
		refs = append(refs, *apc.Spec.TargetRef)
	}
	refs = append(refs, apc.Spec.TargetRefs...)

	return refs
}

// ResolvedTargets returns all resolved target info.
func (apc *AccessPolicyContext) ResolvedTargets() []TargetInfo {
	return apc.resolvedTargets
}

// SuccessfullyResolvedTargets returns only targets that resolved without error.
func (apc *AccessPolicyContext) SuccessfullyResolvedTargets() []TargetInfo {
	var resolved []TargetInfo
	for _, t := range apc.resolvedTargets {
		if t.Resolved && t.Error == nil {
			resolved = append(resolved, t)
		}
	}
	return resolved
}

// FailedTargets returns targets that failed to resolve.
func (apc *AccessPolicyContext) FailedTargets() []TargetInfo {
	var failed []TargetInfo
	for _, t := range apc.resolvedTargets {
		if t.Error != nil {
			failed = append(failed, t)
		}
	}
	return failed
}

// HasFailedTargets returns true if any target failed to resolve.
func (apc *AccessPolicyContext) HasFailedTargets() bool {
	for _, t := range apc.resolvedTargets {
		if t.Error != nil {
			return true
		}
	}
	return false
}

// AllTargetsResolved returns true if all targets resolved successfully.
func (apc *AccessPolicyContext) AllTargetsResolved() bool {
	if len(apc.resolvedTargets) == 0 {
		return false
	}
	for _, t := range apc.resolvedTargets {
		if !t.Resolved || t.Error != nil {
			return false
		}
	}
	return true
}

// RequiresMTLS returns true if mTLS is configured and enabled.
func (apc *AccessPolicyContext) RequiresMTLS() bool {
	return apc.Spec.MTLS != nil && apc.Spec.MTLS.Enabled
}

// RequiresServiceTokens returns true if service tokens are configured.
func (apc *AccessPolicyContext) RequiresServiceTokens() bool {
	return len(apc.Spec.ServiceTokens) > 0
}

// HasCrossNamespaceTargets returns true if any targets are in different namespace.
func (apc *AccessPolicyContext) HasCrossNamespaceTargets() bool {
	for _, t := range apc.resolvedTargets {
		if t.Namespace != apc.Namespace {
			return true
		}
	}
	return false
}

// ExtractHostnames extracts unique hostnames from resolved HTTPRoute/GRPCRoute targets.
func (apc *AccessPolicyContext) ExtractHostnames(
	ctx context.Context,
	k8sClient client.Client,
) ([]string, error) {
	hostnameSet := make(map[string]struct{})

	for _, target := range apc.SuccessfullyResolvedTargets() {
		hostnames, err := extractHostnamesFromTarget(ctx, k8sClient, target)
		if err != nil {
			apc.log.V(1).Info("failed to extract hostnames",
				"target", target.Namespace+"/"+target.Name,
				"kind", target.Kind,
				"error", err.Error(),
			)
			continue
		}
		for _, h := range hostnames {
			hostnameSet[h] = struct{}{}
		}
	}

	var hostnames []string
	for h := range hostnameSet {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)
	return hostnames, nil
}

// -----------------------------------------------------------------------------
// TargetInfo
// -----------------------------------------------------------------------------

// TargetInfo holds information about a resolved policy target.
type TargetInfo struct {
	// Kind of the target resource (HTTPRoute, Gateway, etc.)
	Kind string

	// Namespace of the target resource
	Namespace string

	// Name of the target resource
	Name string

	// Resolved indicates if the target was successfully resolved
	Resolved bool

	// SectionName targets specific listener (Gateway) or rule (Route)
	SectionName *string

	// Error contains the resolution error (nil if Resolved=true)
	Error error
}

// NamespacedName returns the namespaced name for k8s lookups.
func (ti *TargetInfo) NamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: ti.Namespace,
		Name:      ti.Name,
	}
}

// String returns a human-readable representation.
func (ti *TargetInfo) String() string {
	base := fmt.Sprintf("%s/%s/%s", ti.Kind, ti.Namespace, ti.Name)
	if ti.SectionName != nil {
		base += "/" + *ti.SectionName
	}
	return base
}

// IsHTTPRoute returns true if target is an HTTPRoute.
func (ti *TargetInfo) IsHTTPRoute() bool {
	return ti.Kind == "HTTPRoute"
}

// IsGateway returns true if target is a Gateway.
func (ti *TargetInfo) IsGateway() bool {
	return ti.Kind == "Gateway"
}

// -----------------------------------------------------------------------------
// RouteContext
// -----------------------------------------------------------------------------

// RouteContext wraps any route type with attached policies and computed state.
type RouteContext struct {
	// Kind of route (HTTPRoute, TCPRoute, UDPRoute, GRPCRoute)
	Kind string

	// Route is the underlying route object (use type switch to access)
	Route client.Object

	// Namespace and Name for convenience
	Namespace string
	Name      string

	// Computed fields
	attachedPolicies []PolicyRef
	originConfig     *OriginConfig

	// Logger for this context
	log logr.Logger
}

// PolicyRef identifies an attached policy.
type PolicyRef struct {
	Namespace string
	Name      string
	Kind      string // CloudflareAccessPolicy, etc.
}

// OriginConfig holds parsed origin annotations.
type OriginConfig struct {
	Protocol  string        // http, https, tcp, udp
	SSLVerify bool          // whether to verify TLS certificates
	Timeout   time.Duration // connection timeout
	Hostname  string        // For TCPRoute/UDPRoute (from annotation)
}

// NewRouteContext creates a RouteContext for any route type.
func NewRouteContext(route client.Object) *RouteContext {
	kind := route.GetObjectKind().GroupVersionKind().Kind
	// Handle case where GVK isn't set (common with typed objects)
	if kind == "" {
		kind = inferRouteKind(route)
	}
	namespace := route.GetNamespace()
	name := route.GetName()

	log := ctrl.Log.WithName("context").WithName("route").
		WithValues("kind", kind, "route", namespace+"/"+name)

	rc := &RouteContext{
		Kind:      kind,
		Route:     route,
		Namespace: namespace,
		Name:      name,
		log:       log,
	}

	// Parse origin annotations
	rc.originConfig = parseOriginConfig(route.GetAnnotations())
	log.V(1).Info("parsed origin config",
		"protocol", rc.originConfig.Protocol,
		"sslVerify", rc.originConfig.SSLVerify,
	)

	return rc
}

// inferRouteKind infers the route kind from the concrete type.
func inferRouteKind(route client.Object) string {
	switch route.(type) {
	case *gwapiv1.HTTPRoute:
		return "HTTPRoute"
	case *gwapiv1a2.TCPRoute:
		return "TCPRoute"
	case *gwapiv1a2.UDPRoute:
		return "UDPRoute"
	case *gwapiv1.GRPCRoute:
		return "GRPCRoute"
	default:
		return "Unknown"
	}
}

// parseOriginConfig extracts origin settings from annotations.
func parseOriginConfig(annotations map[string]string) *OriginConfig {
	config := &OriginConfig{
		Protocol:  "http",           // default
		SSLVerify: true,             // default
		Timeout:   30 * time.Second, // default
	}

	if v, ok := annotations["cfgate.io/origin-protocol"]; ok {
		config.Protocol = v
	}
	if v, ok := annotations["cfgate.io/origin-ssl-verify"]; ok {
		config.SSLVerify = v == "true"
	}
	if v, ok := annotations["cfgate.io/origin-timeout"]; ok {
		if d, err := time.ParseDuration(v); err == nil {
			config.Timeout = d
		}
	}
	if v, ok := annotations["cfgate.io/hostname"]; ok {
		config.Hostname = v
	}

	return config
}

// AttachedPolicies returns policies attached to this route.
func (rc *RouteContext) AttachedPolicies() []PolicyRef {
	return rc.attachedPolicies
}

// AddAttachedPolicy adds a policy reference.
func (rc *RouteContext) AddAttachedPolicy(ref PolicyRef) {
	rc.attachedPolicies = append(rc.attachedPolicies, ref)
}

// OriginConfig returns parsed origin configuration.
func (rc *RouteContext) OriginConfig() *OriginConfig {
	return rc.originConfig
}

// GetHostnames extracts hostnames from the route.
// Returns annotation hostname for TCP/UDP routes, spec.hostnames for HTTP/GRPC.
func (rc *RouteContext) GetHostnames() []string {
	switch rc.Kind {
	case "TCPRoute", "UDPRoute":
		// TCP/UDP routes use annotation for hostname
		if rc.originConfig.Hostname != "" {
			return []string{rc.originConfig.Hostname}
		}
		return nil

	case "HTTPRoute":
		if hr, ok := rc.Route.(*gwapiv1.HTTPRoute); ok {
			hostnames := make([]string, len(hr.Spec.Hostnames))
			for i, h := range hr.Spec.Hostnames {
				hostnames[i] = string(h)
			}
			return hostnames
		}

	case "GRPCRoute":
		if gr, ok := rc.Route.(*gwapiv1.GRPCRoute); ok {
			hostnames := make([]string, len(gr.Spec.Hostnames))
			for i, h := range gr.Spec.Hostnames {
				hostnames[i] = string(h)
			}
			return hostnames
		}
	}

	return nil
}

// HasAccessPolicyAnnotation returns true if access-policy annotation is set.
func (rc *RouteContext) HasAccessPolicyAnnotation() bool {
	_, ok := rc.Route.GetAnnotations()["cfgate.io/access-policy"]
	return ok
}

// GetAccessPolicyAnnotation returns the access-policy annotation value.
func (rc *RouteContext) GetAccessPolicyAnnotation() string {
	return rc.Route.GetAnnotations()["cfgate.io/access-policy"]
}

// -----------------------------------------------------------------------------
// Builder Functions
// -----------------------------------------------------------------------------

// BuildTunnelContext creates a TunnelContext with full initialization.
// Returns nil and logs warning if tunnel not found.
func BuildTunnelContext(
	ctx context.Context,
	k8sClient client.Client,
	cfClient cloudflare.Client,
	ref types.NamespacedName,
	accountID string,
) (*TunnelContext, error) {
	log := ctrl.Log.WithName("context").WithValues("tunnel", ref)

	// Fetch tunnel
	var tunnel cfgatev1alpha1.CloudflareTunnel
	if err := k8sClient.Get(ctx, ref, &tunnel); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("tunnel not found")
			return nil, nil
		}
		return nil, fmt.Errorf("fetching tunnel: %w", err)
	}

	// Create DNS service if DNS enabled
	var dnsClient *cloudflare.DNSService
	if tunnel.Spec.DNS != nil && tunnel.Spec.DNS.Enabled {
		dnsClient = cloudflare.NewDNSService(cfClient)
		log.V(1).Info("DNS client created", "zones", len(tunnel.Spec.DNS.Zones))
	}

	return NewTunnelContext(&tunnel, accountID, cfClient, dnsClient), nil
}

// BuildAccessPolicyContext creates an AccessPolicyContext with full initialization.
func BuildAccessPolicyContext(
	ctx context.Context,
	k8sClient client.Client,
	ref types.NamespacedName,
) (*AccessPolicyContext, error) {
	log := ctrl.Log.WithName("context").WithValues("policy", ref)

	// Fetch policy
	var policy cfgatev1alpha1.CloudflareAccessPolicy
	if err := k8sClient.Get(ctx, ref, &policy); err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("policy not found")
			return nil, nil
		}
		return nil, fmt.Errorf("fetching policy: %w", err)
	}

	// Build context
	return NewAccessPolicyContext(ctx, &policy, k8sClient), nil
}

// BuildRouteContext creates a RouteContext for any route type.
func BuildRouteContext(
	ctx context.Context,
	k8sClient client.Client,
	kind string,
	ref types.NamespacedName,
) (*RouteContext, error) {
	log := ctrl.Log.WithName("context").WithValues("kind", kind, "route", ref)

	var route client.Object
	var err error

	switch kind {
	case "HTTPRoute":
		hr := &gwapiv1.HTTPRoute{}
		err = k8sClient.Get(ctx, ref, hr)
		route = hr
	case "TCPRoute":
		tr := &gwapiv1a2.TCPRoute{}
		err = k8sClient.Get(ctx, ref, tr)
		route = tr
	case "UDPRoute":
		ur := &gwapiv1a2.UDPRoute{}
		err = k8sClient.Get(ctx, ref, ur)
		route = ur
	case "GRPCRoute":
		gr := &gwapiv1.GRPCRoute{}
		err = k8sClient.Get(ctx, ref, gr)
		route = gr
	default:
		return nil, fmt.Errorf("unsupported route kind: %s", kind)
	}

	if err != nil {
		if apierrors.IsNotFound(err) {
			log.V(1).Info("route not found")
			return nil, nil
		}
		return nil, fmt.Errorf("fetching route: %w", err)
	}

	return NewRouteContext(route), nil
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// resolveTargets resolves all targetRefs to TargetInfo.
func resolveTargets(
	ctx context.Context,
	policy *cfgatev1alpha1.CloudflareAccessPolicy,
	k8sClient client.Client,
	log logr.Logger,
) []TargetInfo {
	var targets []TargetInfo

	// Handle single targetRef
	if policy.Spec.TargetRef != nil {
		target := resolveTarget(ctx, policy.Spec.TargetRef, policy.Namespace, k8sClient, log)
		targets = append(targets, target)
	}

	// Handle multiple targetRefs
	for i := range policy.Spec.TargetRefs {
		target := resolveTarget(ctx, &policy.Spec.TargetRefs[i], policy.Namespace, k8sClient, log)
		targets = append(targets, target)
	}

	return targets
}

// resolveTarget resolves a single PolicyTargetReference.
func resolveTarget(
	ctx context.Context,
	ref *cfgatev1alpha1.PolicyTargetReference,
	defaultNamespace string,
	k8sClient client.Client,
	log logr.Logger,
) TargetInfo {
	namespace := defaultNamespace
	if ref.Namespace != nil && *ref.Namespace != "" {
		namespace = *ref.Namespace
	}

	info := TargetInfo{
		Kind:        ref.Kind,
		Namespace:   namespace,
		Name:        ref.Name,
		SectionName: ref.SectionName,
		Resolved:    false,
	}

	// Check if target exists
	exists, err := targetExists(ctx, k8sClient, ref.Kind, namespace, ref.Name)
	if err != nil {
		log.V(1).Info("target resolution failed",
			"kind", ref.Kind,
			"namespace", namespace,
			"name", ref.Name,
			"error", err.Error(),
		)
		info.Error = err
		return info
	}

	if !exists {
		info.Error = fmt.Errorf("target %s/%s not found", namespace, ref.Name)
		return info
	}

	// Check ReferenceGrant if cross-namespace
	if namespace != defaultNamespace {
		granted, err := checkReferenceGrant(ctx, k8sClient, defaultNamespace, namespace, ref.Kind)
		if err != nil {
			info.Error = fmt.Errorf("checking ReferenceGrant: %w", err)
			return info
		}
		if !granted {
			info.Error = fmt.Errorf("cross-namespace reference to %s/%s not permitted by ReferenceGrant",
				namespace, ref.Name)
			return info
		}
	}

	info.Resolved = true
	return info
}

// targetExists checks if a target resource exists.
func targetExists(
	ctx context.Context,
	k8sClient client.Client,
	kind, namespace, name string,
) (bool, error) {
	var obj client.Object

	switch kind {
	case "Gateway":
		obj = &gwapiv1.Gateway{}
	case "HTTPRoute":
		obj = &gwapiv1.HTTPRoute{}
	case "GRPCRoute":
		obj = &gwapiv1.GRPCRoute{}
	case "TCPRoute":
		obj = &gwapiv1a2.TCPRoute{}
	case "UDPRoute":
		obj = &gwapiv1a2.UDPRoute{}
	default:
		return false, fmt.Errorf("unsupported target kind: %s", kind)
	}

	key := types.NamespacedName{Namespace: namespace, Name: name}
	if err := k8sClient.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// checkReferenceGrant checks if cross-namespace reference is permitted.
func checkReferenceGrant(
	ctx context.Context,
	k8sClient client.Client,
	fromNamespace, toNamespace, targetKind string,
) (bool, error) {
	var grants gwapiv1b1.ReferenceGrantList
	if err := k8sClient.List(ctx, &grants, client.InNamespace(toNamespace)); err != nil {
		return false, err
	}

	for _, grant := range grants.Items {
		for _, from := range grant.Spec.From {
			if from.Group != "cfgate.io" {
				continue
			}
			if from.Kind != "CloudflareAccessPolicy" {
				continue
			}
			// Namespace is a required string type in ReferenceGrantFrom
			if string(from.Namespace) != fromNamespace {
				continue
			}

			// Check if target kind is permitted
			for _, to := range grant.Spec.To {
				if to.Group == "gateway.networking.k8s.io" &&
					string(to.Kind) == targetKind {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// extractHostnamesFromTarget extracts hostnames from a resolved target.
func extractHostnamesFromTarget(
	ctx context.Context,
	k8sClient client.Client,
	target TargetInfo,
) ([]string, error) {
	switch target.Kind {
	case "HTTPRoute":
		var route gwapiv1.HTTPRoute
		if err := k8sClient.Get(ctx, target.NamespacedName(), &route); err != nil {
			return nil, err
		}
		hostnames := make([]string, len(route.Spec.Hostnames))
		for i, h := range route.Spec.Hostnames {
			hostnames[i] = string(h)
		}
		return hostnames, nil

	case "GRPCRoute":
		var route gwapiv1.GRPCRoute
		if err := k8sClient.Get(ctx, target.NamespacedName(), &route); err != nil {
			return nil, err
		}
		hostnames := make([]string, len(route.Spec.Hostnames))
		for i, h := range route.Spec.Hostnames {
			hostnames[i] = string(h)
		}
		return hostnames, nil

	case "Gateway":
		var gw gwapiv1.Gateway
		if err := k8sClient.Get(ctx, target.NamespacedName(), &gw); err != nil {
			return nil, err
		}
		// Extract hostnames from listeners
		var hostnames []string
		for _, listener := range gw.Spec.Listeners {
			if listener.Hostname != nil {
				hostnames = append(hostnames, string(*listener.Hostname))
			}
		}
		return hostnames, nil

	default:
		// TCPRoute/UDPRoute don't have hostnames in spec
		return nil, nil
	}
}

// countResolved counts targets that resolved successfully.
func countResolved(targets []TargetInfo) int {
	count := 0
	for _, t := range targets {
		if t.Resolved && t.Error == nil {
			count++
		}
	}
	return count
}

// countFailed counts targets that failed to resolve.
func countFailed(targets []TargetInfo) int {
	count := 0
	for _, t := range targets {
		if t.Error != nil {
			count++
		}
	}
	return count
}
