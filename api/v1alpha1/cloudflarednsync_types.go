// Package v1alpha1 contains API Schema definitions for the cfgate v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TunnelRef references a CloudflareTunnel resource.
type TunnelRef struct {
	// Name is the name of the CloudflareTunnel.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the CloudflareTunnel.
	// Defaults to the CloudflareDNSSync's namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ZoneConfig defines a DNS zone to manage.
type ZoneConfig struct {
	// Name is the zone name (e.g., example.com).
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// ID is the optional explicit zone ID (skips lookup).
	// +optional
	ID string `json:"id,omitempty"`
}

// GatewayRoutesSource configures watching Gateway API routes for hostnames.
type GatewayRoutesSource struct {
	// Enabled enables watching Gateway API routes.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// AnnotationFilter only syncs routes with this annotation.
	// +optional
	AnnotationFilter string `json:"annotationFilter,omitempty"`
}

// ExplicitHostname defines an explicit hostname to sync.
type ExplicitHostname struct {
	// Hostname is the DNS hostname to create.
	// +kubebuilder:validation:Required
	Hostname string `json:"hostname"`

	// Target is the CNAME target. Supports template variable {{ .TunnelDomain }}.
	// +kubebuilder:validation:Required
	Target string `json:"target"`

	// Proxied enables Cloudflare proxy for this record.
	// +kubebuilder:default=true
	Proxied bool `json:"proxied,omitempty"`

	// TTL is the DNS record TTL. Use "auto" for Cloudflare automatic TTL.
	// +kubebuilder:default="auto"
	TTL string `json:"ttl,omitempty"`
}

// HostnameSource defines sources for hostnames to sync.
type HostnameSource struct {
	// GatewayRoutes configures watching Gateway API routes.
	// +optional
	GatewayRoutes GatewayRoutesSource `json:"gatewayRoutes,omitempty"`

	// Explicit defines explicit hostnames to sync.
	// +optional
	Explicit []ExplicitHostname `json:"explicit,omitempty"`
}

// RecordDefaults defines default settings for DNS records.
type RecordDefaults struct {
	// Proxied enables Cloudflare proxy by default.
	// +kubebuilder:default=true
	Proxied bool `json:"proxied,omitempty"`

	// TTL is the default DNS record TTL.
	// +kubebuilder:default="auto"
	TTL string `json:"ttl,omitempty"`
}

// TXTRecordOwnership configures TXT record-based ownership tracking.
type TXTRecordOwnership struct {
	// Enabled enables TXT record ownership tracking.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// Prefix is the prefix for TXT record names.
	// +kubebuilder:default="_cfgate"
	Prefix string `json:"prefix,omitempty"`
}

// CommentOwnership configures comment-based ownership tracking.
type CommentOwnership struct {
	// Enabled enables comment-based ownership tracking.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Template is the comment template. Supports template variables.
	// +kubebuilder:default="managed by cfgate, tunnel={{ .TunnelName }}"
	Template string `json:"template,omitempty"`
}

// OwnershipConfig defines how to track record ownership.
type OwnershipConfig struct {
	// TXTRecord configures TXT record-based ownership.
	// +optional
	TXTRecord TXTRecordOwnership `json:"txtRecord,omitempty"`

	// Comment configures comment-based ownership.
	// +optional
	Comment CommentOwnership `json:"comment,omitempty"`
}

// CleanupPolicy defines what to do when records are no longer needed.
type CleanupPolicy struct {
	// DeleteOnRouteRemoval deletes records when the route is deleted.
	// +kubebuilder:default=true
	DeleteOnRouteRemoval bool `json:"deleteOnRouteRemoval,omitempty"`

	// DeleteOnResourceRemoval deletes records when the DNSSync resource is deleted.
	// +kubebuilder:default=true
	DeleteOnResourceRemoval bool `json:"deleteOnResourceRemoval,omitempty"`

	// OnlyManaged only deletes records that were created by cfgate.
	// +kubebuilder:default=true
	OnlyManaged bool `json:"onlyManaged,omitempty"`
}

// CloudflareDNSSyncSpec defines the desired state of CloudflareDNSSync.
type CloudflareDNSSyncSpec struct {
	// TunnelRef references the CloudflareTunnel to sync DNS for.
	// +kubebuilder:validation:Required
	TunnelRef TunnelRef `json:"tunnelRef"`

	// Zones defines the DNS zones to manage.
	// +optional
	Zones []ZoneConfig `json:"zones,omitempty"`

	// Source defines where to get hostnames to sync.
	// +optional
	Source HostnameSource `json:"source,omitempty"`

	// Defaults defines default settings for DNS records.
	// +optional
	Defaults RecordDefaults `json:"defaults,omitempty"`

	// Ownership defines how to track record ownership.
	// +optional
	Ownership OwnershipConfig `json:"ownership,omitempty"`

	// CleanupPolicy defines cleanup behavior for records.
	// +optional
	CleanupPolicy CleanupPolicy `json:"cleanupPolicy,omitempty"`

	// FallbackCredentialsRef references a secret containing fallback Cloudflare API credentials.
	// Used during deletion when the referenced tunnel's credentials are unavailable.
	// This enables cleanup of DNS records even if the tunnel or its secret is deleted.
	// The secret must contain CLOUDFLARE_API_TOKEN key.
	// +optional
	FallbackCredentialsRef *SecretReference `json:"fallbackCredentialsRef,omitempty"`
}

// DNSRecordStatus represents the status of a single DNS record.
type DNSRecordStatus struct {
	// Hostname is the DNS hostname.
	Hostname string `json:"hostname"`

	// Type is the DNS record type (e.g., CNAME).
	Type string `json:"type"`

	// Target is the record target/content.
	Target string `json:"target"`

	// Proxied indicates if Cloudflare proxy is enabled.
	Proxied bool `json:"proxied"`

	// Status is the sync status: Synced, Pending, Failed.
	Status string `json:"status"`

	// RecordID is the Cloudflare record ID.
	// +optional
	RecordID string `json:"recordId,omitempty"`

	// Error contains the error message if status is Failed.
	// +optional
	Error string `json:"error,omitempty"`
}

// CloudflareDNSSyncStatus defines the observed state of CloudflareDNSSync.
type CloudflareDNSSyncStatus struct {
	// SyncedRecords is the number of successfully synced records.
	SyncedRecords int32 `json:"syncedRecords,omitempty"`

	// PendingRecords is the number of records pending sync.
	PendingRecords int32 `json:"pendingRecords,omitempty"`

	// FailedRecords is the number of records that failed to sync.
	FailedRecords int32 `json:"failedRecords,omitempty"`

	// Records contains the status of individual DNS records.
	// +optional
	Records []DNSRecordStatus `json:"records,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastSyncTime is the last time records were synced.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest available observations of the sync's state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cfdns;dnsync
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Synced",type="integer",JSONPath=".status.syncedRecords"
// +kubebuilder:printcolumn:name="Pending",type="integer",JSONPath=".status.pendingRecords"
// +kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.failedRecords"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CloudflareDNSSync is the Schema for the cloudflarednssyncs API.
// It manages DNS records for CloudflareTunnel resources.
type CloudflareDNSSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudflareDNSSyncSpec   `json:"spec,omitempty"`
	Status CloudflareDNSSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudflareDNSSyncList contains a list of CloudflareDNSSync.
type CloudflareDNSSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudflareDNSSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudflareDNSSync{}, &CloudflareDNSSyncList{})
}
