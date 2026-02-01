// Package controller contains the reconciliation logic for cfgate CRDs.
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	cfgatev1alpha1 "cfgate.io/cfgate/api/v1alpha1"
)

// Route annotation keys for per-route origin configuration.
const (
	AnnotationOriginConnectTimeout = "cfgate.io/origin-connect-timeout"
	AnnotationOriginNoTLSVerify    = "cfgate.io/origin-no-tls-verify"
	AnnotationOriginHTTPHostHeader = "cfgate.io/origin-http-host-header"
	AnnotationOriginServerName     = "cfgate.io/origin-server-name"
	AnnotationOriginCAPool         = "cfgate.io/origin-ca-pool"
	AnnotationOriginHTTP2          = "cfgate.io/origin-http2"
	AnnotationOriginMatchSNIToHost = "cfgate.io/origin-match-sni-to-host"
)

// HTTPRouteReconciler reconciles HTTPRoute resources.
// It validates routes against Gateway configuration and triggers
// tunnel configuration syncs when routes change.
type HTTPRouteReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

// Reconcile handles the reconciliation loop for HTTPRoute resources.
// It validates the route against parent Gateways and triggers config sync.
func (r *HTTPRouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling HTTPRoute", "name", req.Name, "namespace", req.Namespace)

	// 1. Fetch HTTPRoute resource
	var route gwapiv1.HTTPRoute
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("HTTPRoute not found, ignoring")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get HTTPRoute: %w", err)
	}

	// 2. For each parentRef, validate Gateway exists and accepts route
	var parentStatuses []gwapiv1.RouteParentStatus

	for _, parentRef := range route.Spec.ParentRefs {
		accepted, reason, err := r.validateParentRef(ctx, &route, parentRef)
		if err != nil {
			log.Error(err, "failed to validate parent ref")
		}

		// Build parent status
		parentNS := gwapiv1.Namespace(route.Namespace)
		if parentRef.Namespace != nil {
			parentNS = *parentRef.Namespace
		}

		status := gwapiv1.RouteParentStatus{
			ParentRef: gwapiv1.ParentReference{
				Group:       parentRef.Group,
				Kind:        parentRef.Kind,
				Namespace:   &parentNS,
				Name:        parentRef.Name,
				SectionName: parentRef.SectionName,
			},
			ControllerName: GatewayControllerName,
			Conditions: []metav1.Condition{
				{
					Type:               string(gwapiv1.RouteConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             "Accepted",
					Message:            "Route accepted by Gateway",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: route.Generation,
				},
				{
					Type:               string(gwapiv1.RouteConditionResolvedRefs),
					Status:             metav1.ConditionTrue,
					Reason:             "ResolvedRefs",
					Message:            "All references resolved",
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: route.Generation,
				},
			},
		}

		if !accepted {
			status.Conditions[0].Status = metav1.ConditionFalse
			status.Conditions[0].Reason = reason
			status.Conditions[0].Message = err.Error()
		}

		parentStatuses = append(parentStatuses, status)
	}

	// 3. Resolve backend services
	if err := r.resolveBackends(ctx, &route); err != nil {
		log.Error(err, "failed to resolve backends")
		// Update ResolvedRefs condition
		for i := range parentStatuses {
			parentStatuses[i].Conditions[1].Status = metav1.ConditionFalse
			parentStatuses[i].Conditions[1].Reason = "BackendNotFound"
			parentStatuses[i].Conditions[1].Message = err.Error()
		}
	}

	// 4. Update route status
	route.Status.Parents = parentStatuses
	if err := r.Status().Update(ctx, &route); err != nil {
		log.Error(err, "failed to update route status")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	r.Recorder.Event(&route, corev1.EventTypeNormal, "Reconciled", "HTTPRoute reconciled successfully")
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HTTPRouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gwapiv1.HTTPRoute{}).
		Complete(r)
}

// validateParentRef validates that the parent Gateway accepts this route.
// Returns true if the route is accepted by the Gateway.
func (r *HTTPRouteReconciler) validateParentRef(ctx context.Context, route *gwapiv1.HTTPRoute, ref gwapiv1.ParentReference) (bool, string, error) {
	// Get the Gateway
	gwNamespace := route.Namespace
	if ref.Namespace != nil {
		gwNamespace = string(*ref.Namespace)
	}

	var gateway gwapiv1.Gateway
	if err := r.Get(ctx, types.NamespacedName{
		Name:      string(ref.Name),
		Namespace: gwNamespace,
	}, &gateway); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "NoMatchingParent", fmt.Errorf("gateway %s/%s not found", gwNamespace, ref.Name)
		}
		return false, "Error", err
	}

	// Check if Gateway's GatewayClass is ours
	var gc gwapiv1.GatewayClass
	if err := r.Get(ctx, types.NamespacedName{Name: string(gateway.Spec.GatewayClassName)}, &gc); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "NoMatchingParent", fmt.Errorf("gateway class %s not found", gateway.Spec.GatewayClassName)
		}
		return false, "Error", err
	}

	if string(gc.Spec.ControllerName) != GatewayControllerName {
		// Not our Gateway, skip
		return false, "NoMatchingParent", fmt.Errorf("gateway is not managed by cfgate")
	}

	// Check if Gateway has tunnel reference
	if _, ok := gateway.Annotations[AnnotationTunnelRef]; !ok {
		return false, "NoTunnelRef", fmt.Errorf("gateway has no tunnel reference")
	}

	// Check listener compatibility if section name specified
	if ref.SectionName != nil {
		found := false
		for _, listener := range gateway.Spec.Listeners {
			if listener.Name == *ref.SectionName {
				found = true
				// Check allowed routes
				if listener.AllowedRoutes != nil {
					// Check namespace selector
					if listener.AllowedRoutes.Namespaces != nil {
						from := listener.AllowedRoutes.Namespaces.From
						if from != nil && *from == gwapiv1.NamespacesFromSame {
							if route.Namespace != gateway.Namespace {
								return false, "NotAllowedByListeners", fmt.Errorf("route namespace not allowed by listener")
							}
						}
					}
				}
				break
			}
		}
		if !found {
			return false, "NoMatchingListenerHostname", fmt.Errorf("listener %s not found", *ref.SectionName)
		}
	}

	return true, "", nil
}

// resolveBackends resolves backend service references to endpoints.
// Returns an error if any required backend cannot be resolved.
func (r *HTTPRouteReconciler) resolveBackends(ctx context.Context, route *gwapiv1.HTTPRoute) error {
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.BackendRefs {
			// Skip non-Service backends
			if backend.Kind != nil && *backend.Kind != "Service" {
				continue
			}

			// Get the service
			namespace := route.Namespace
			if backend.Namespace != nil {
				namespace = string(*backend.Namespace)
			}

			var svc corev1.Service
			if err := r.Get(ctx, types.NamespacedName{
				Name:      string(backend.Name),
				Namespace: namespace,
			}, &svc); err != nil {
				if apierrors.IsNotFound(err) {
					return fmt.Errorf("service %s/%s not found", namespace, backend.Name)
				}
				return fmt.Errorf("failed to get service: %w", err)
			}
		}
	}

	return nil
}

// buildIngressRule builds a cloudflared ingress rule from an HTTPRoute rule.
// Includes hostname, path matching, and backend configuration.
func (r *HTTPRouteReconciler) buildIngressRule(ctx context.Context, route *gwapiv1.HTTPRoute, rule gwapiv1.HTTPRouteRule) (*IngressRule, error) {
	if len(rule.BackendRefs) == 0 {
		return nil, fmt.Errorf("no backends specified")
	}

	backend := rule.BackendRefs[0] // Use first backend (no weighted routing support)

	namespace := route.Namespace
	if backend.Namespace != nil {
		namespace = string(*backend.Namespace)
	}

	port := int32(80)
	if backend.Port != nil {
		port = int32(*backend.Port)
	}

	service := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", backend.Name, namespace, port)

	// Get path if specified
	path := ""
	pathType := "Prefix"
	if len(rule.Matches) > 0 && rule.Matches[0].Path != nil {
		if rule.Matches[0].Path.Value != nil {
			path = *rule.Matches[0].Path.Value
		}
		if rule.Matches[0].Path.Type != nil {
			pathType = string(*rule.Matches[0].Path.Type)
		}
	}

	ingressRule := &IngressRule{
		Path:     path,
		PathType: pathType,
		Service:  service,
	}

	// Build origin config from annotations
	ingressRule.OriginRequest = &OriginRequestConfig{}

	if v, ok := route.Annotations[AnnotationOriginConnectTimeout]; ok {
		ingressRule.OriginRequest.ConnectTimeout = v
	}
	if v, ok := route.Annotations[AnnotationOriginNoTLSVerify]; ok && strings.ToLower(v) == "true" {
		ingressRule.OriginRequest.NoTLSVerify = true
	}
	if v, ok := route.Annotations[AnnotationOriginHTTPHostHeader]; ok {
		ingressRule.OriginRequest.HTTPHostHeader = v
	}
	if v, ok := route.Annotations[AnnotationOriginServerName]; ok {
		ingressRule.OriginRequest.OriginServerName = v
	}
	if v, ok := route.Annotations[AnnotationOriginCAPool]; ok {
		ingressRule.OriginRequest.CAPool = v
	}
	if v, ok := route.Annotations[AnnotationOriginHTTP2]; ok && strings.ToLower(v) == "true" {
		ingressRule.OriginRequest.HTTP2Origin = true
	}
	if v, ok := route.Annotations[AnnotationOriginMatchSNIToHost]; ok && strings.ToLower(v) == "true" {
		ingressRule.OriginRequest.MatchSNIToHost = true
	}

	return ingressRule, nil
}

// updateRouteStatus updates the HTTPRoute status for a specific parent.
func (r *HTTPRouteReconciler) updateRouteStatus(ctx context.Context, route *gwapiv1.HTTPRoute, ref gwapiv1.ParentReference, accepted bool, reason, message string) error {
	// Find or create parent status
	parentNS := gwapiv1.Namespace(route.Namespace)
	if ref.Namespace != nil {
		parentNS = *ref.Namespace
	}

	var found bool
	for i, ps := range route.Status.Parents {
		if ps.ParentRef.Name == ref.Name &&
			(ps.ParentRef.Namespace == nil && parentNS == gwapiv1.Namespace(route.Namespace) ||
				ps.ParentRef.Namespace != nil && *ps.ParentRef.Namespace == parentNS) {
			// Update existing
			status := metav1.ConditionTrue
			if !accepted {
				status = metav1.ConditionFalse
			}
			route.Status.Parents[i].Conditions = []metav1.Condition{
				{
					Type:               string(gwapiv1.RouteConditionAccepted),
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: route.Generation,
				},
			}
			found = true
			break
		}
	}

	if !found {
		status := metav1.ConditionTrue
		if !accepted {
			status = metav1.ConditionFalse
		}
		route.Status.Parents = append(route.Status.Parents, gwapiv1.RouteParentStatus{
			ParentRef: gwapiv1.ParentReference{
				Name:      ref.Name,
				Namespace: &parentNS,
			},
			ControllerName: GatewayControllerName,
			Conditions: []metav1.Condition{
				{
					Type:               string(gwapiv1.RouteConditionAccepted),
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: metav1.Now(),
					ObservedGeneration: route.Generation,
				},
			},
		})
	}

	return r.Status().Update(ctx, route)
}

// findTunnelForRoute finds the CloudflareTunnel associated with an HTTPRoute.
// Traverses parentRef -> Gateway -> tunnel-ref annotation.
func (r *HTTPRouteReconciler) findTunnelForRoute(ctx context.Context, route *gwapiv1.HTTPRoute) (*cfgatev1alpha1.CloudflareTunnel, error) {
	for _, parentRef := range route.Spec.ParentRefs {
		gwNamespace := route.Namespace
		if parentRef.Namespace != nil {
			gwNamespace = string(*parentRef.Namespace)
		}

		var gateway gwapiv1.Gateway
		if err := r.Get(ctx, types.NamespacedName{
			Name:      string(parentRef.Name),
			Namespace: gwNamespace,
		}, &gateway); err != nil {
			continue
		}

		tunnelRef, ok := gateway.Annotations[AnnotationTunnelRef]
		if !ok {
			continue
		}

		parts := strings.Split(tunnelRef, "/")
		if len(parts) != 2 {
			continue
		}

		var tunnel cfgatev1alpha1.CloudflareTunnel
		if err := r.Get(ctx, types.NamespacedName{
			Name:      parts[1],
			Namespace: parts[0],
		}, &tunnel); err != nil {
			continue
		}

		return &tunnel, nil
	}

	return nil, fmt.Errorf("no tunnel found for route")
}

// IngressRule represents a cloudflared ingress rule derived from an HTTPRoute.
type IngressRule struct {
	// Hostname is the hostname to match.
	Hostname string

	// Path is the path prefix or regex to match.
	Path string

	// PathType is the type of path matching (Prefix, Exact, RegularExpression).
	PathType string

	// Service is the backend service URL (e.g., http://app:8080).
	Service string

	// OriginRequest contains per-rule origin configuration.
	OriginRequest *OriginRequestConfig
}

// OriginRequestConfig contains origin-specific settings for a rule.
type OriginRequestConfig struct {
	ConnectTimeout   string
	NoTLSVerify      bool
	HTTPHostHeader   string
	OriginServerName string
	CAPool           string
	HTTP2Origin      bool
	MatchSNIToHost   bool
}
