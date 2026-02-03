// Package v1alpha1 contains API Schema definitions for the cfgate v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PolicyTargetReference identifies a target for policy attachment.
// Based on Gateway API LocalPolicyTargetReferenceWithSectionName.
// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="group must be gateway.networking.k8s.io"
// +kubebuilder:validation:XValidation:rule="self.kind in ['Gateway', 'HTTPRoute', 'GRPCRoute', 'TCPRoute', 'UDPRoute']",message="kind must be Gateway, HTTPRoute, GRPCRoute, TCPRoute, or UDPRoute"
type PolicyTargetReference struct {
	// Group is the API group of the target resource.
	// +kubebuilder:default="gateway.networking.k8s.io"
	Group string `json:"group"`

	// Kind is the kind of the target resource.
	// +kubebuilder:validation:Enum=Gateway;HTTPRoute;GRPCRoute;TCPRoute;UDPRoute
	Kind string `json:"kind"`

	// Name is the name of the target resource.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Namespace is the namespace of the target resource.
	// Cross-namespace targeting requires ReferenceGrant.
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// SectionName targets specific listener (Gateway) or rule (Route).
	// +optional
	SectionName *string `json:"sectionName,omitempty"`
}

// CloudflareSecretRef references Cloudflare credentials.
type CloudflareSecretRef struct {
	// Name of the secret containing credentials.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the secret (defaults to policy namespace).
	// +optional
	Namespace *string `json:"namespace,omitempty"`

	// AccountID is the Cloudflare account ID.
	// +optional
	AccountID string `json:"accountId,omitempty"`

	// AccountName is the Cloudflare account name (looked up via API).
	// +optional
	AccountName string `json:"accountName,omitempty"`
}

// AccessApplication defines Cloudflare Access Application settings.
type AccessApplication struct {
	// Name is the display name in Cloudflare dashboard.
	// Defaults to CR name if omitted.
	// +optional
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name,omitempty"`

	// Domain is the protected domain (auto-generated from routes if omitted).
	// +optional
	Domain string `json:"domain,omitempty"`

	// Path restricts protection to specific path prefix.
	// +optional
	// +kubebuilder:default="/"
	Path string `json:"path,omitempty"`

	// SessionDuration controls session cookie lifetime.
	// +optional
	// +kubebuilder:default="24h"
	// +kubebuilder:validation:Pattern=`^[0-9]+(h|m|s)$`
	SessionDuration string `json:"sessionDuration,omitempty"`

	// Type is the application type.
	// +kubebuilder:validation:Enum=self_hosted;saas;ssh;vnc;browser_isolation
	// +kubebuilder:default=self_hosted
	Type string `json:"type,omitempty"`

	// LogoURL is the application logo in dashboard.
	// +optional
	LogoURL string `json:"logoUrl,omitempty"`

	// SkipInterstitial bypasses the Access login page for API requests.
	// +optional
	// +kubebuilder:default=false
	SkipInterstitial bool `json:"skipInterstitial,omitempty"`

	// EnableBindingCookie enables binding cookies for sticky sessions.
	// +optional
	// +kubebuilder:default=false
	EnableBindingCookie bool `json:"enableBindingCookie,omitempty"`

	// HttpOnlyCookieAttribute adds HttpOnly to session cookies.
	// +optional
	// +kubebuilder:default=true
	HttpOnlyCookieAttribute bool `json:"httpOnlyCookieAttribute,omitempty"`

	// SameSiteCookieAttribute controls cross-site cookie behavior.
	// +kubebuilder:validation:Enum=strict;lax;none
	// +kubebuilder:default=lax
	SameSiteCookieAttribute string `json:"sameSiteCookieAttribute,omitempty"`

	// CustomDenyMessage shown when access is denied.
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	CustomDenyMessage string `json:"customDenyMessage,omitempty"`

	// CustomDenyURL redirects to this URL when denied (instead of message).
	// +optional
	CustomDenyURL string `json:"customDenyUrl,omitempty"`
}

// AccessPolicyRule defines an access allow/deny rule.
type AccessPolicyRule struct {
	// Name is a human-readable identifier.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Decision is the policy action.
	// +kubebuilder:validation:Enum=allow;deny;bypass;non_identity
	// +kubebuilder:default=allow
	Decision string `json:"decision"`

	// Precedence determines rule evaluation order (lower = first).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=9999
	// +optional
	Precedence *int `json:"precedence,omitempty"`

	// Include rules (ANY must match for rule to apply).
	// +optional
	Include []AccessRule `json:"include,omitempty"`

	// Exclude rules (if ANY match, rule does not apply).
	// +optional
	Exclude []AccessRule `json:"exclude,omitempty"`

	// Require rules (ALL must match for rule to apply).
	// +optional
	Require []AccessRule `json:"require,omitempty"`

	// SessionDuration overrides application session duration for this rule.
	// +optional
	SessionDuration string `json:"sessionDuration,omitempty"`

	// PurposeJustificationRequired requires user to provide justification.
	// +optional
	// +kubebuilder:default=false
	PurposeJustificationRequired bool `json:"purposeJustificationRequired,omitempty"`

	// PurposeJustificationPrompt is the prompt shown to user.
	// +optional
	PurposeJustificationPrompt string `json:"purposeJustificationPrompt,omitempty"`

	// ApprovalRequired requires approval from specific users.
	// +optional
	// +kubebuilder:default=false
	ApprovalRequired bool `json:"approvalRequired,omitempty"`

	// ApprovalGroups defines who can approve access.
	// +optional
	ApprovalGroups []ApprovalGroup `json:"approvalGroups,omitempty"`
}

// AccessRule defines identity matching criteria.
// +kubebuilder:validation:XValidation:rule="[has(self.email), has(self.emailDomain), has(self.emailListRef), has(self.ipRange), has(self.country), has(self.everyone), has(self.certificate), has(self.commonName), has(self.serviceToken), has(self.groupRef), has(self.gsuite), has(self.github), has(self.azure), has(self.okta), has(self.saml)].exists(x, x)",message="at least one rule type must be specified"
type AccessRule struct {
	// Email matches specific email addresses.
	// +optional
	Email *EmailRule `json:"email,omitempty"`

	// EmailDomain matches email domain suffix.
	// +optional
	EmailDomain *EmailDomainRule `json:"emailDomain,omitempty"`

	// EmailListRef references a Cloudflare Access list.
	// +optional
	EmailListRef *AccessListRef `json:"emailListRef,omitempty"`

	// IPRange matches source IP CIDR ranges.
	// +optional
	IPRange *IPRangeRule `json:"ipRange,omitempty"`

	// Country matches source country codes (ISO 3166-1 alpha-2).
	// +optional
	Country *CountryRule `json:"country,omitempty"`

	// Everyone matches all users (use with caution).
	// +optional
	Everyone *bool `json:"everyone,omitempty"`

	// Certificate requires valid mTLS certificate.
	// +optional
	Certificate *bool `json:"certificate,omitempty"`

	// CommonName matches certificate common name.
	// +optional
	CommonName *CommonNameRule `json:"commonName,omitempty"`

	// ServiceToken requires valid service token.
	// +optional
	ServiceToken *bool `json:"serviceToken,omitempty"`

	// GroupRef references an AccessGroup CR.
	// +optional
	GroupRef *AccessGroupRef `json:"groupRef,omitempty"`

	// GSuite matches Google Workspace groups.
	// +optional
	GSuite *GSuiteRule `json:"gsuite,omitempty"`

	// GitHub matches GitHub organization membership.
	// +optional
	GitHub *GitHubRule `json:"github,omitempty"`

	// Azure matches Azure AD groups.
	// +optional
	Azure *AzureRule `json:"azure,omitempty"`

	// Okta matches Okta groups.
	// +optional
	Okta *OktaRule `json:"okta,omitempty"`

	// SAML matches SAML assertion attributes.
	// +optional
	SAML *SAMLRule `json:"saml,omitempty"`
}

// EmailRule matches specific email addresses.
type EmailRule struct {
	// Addresses to match.
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`
}

// EmailDomainRule matches email domain suffix.
type EmailDomainRule struct {
	// Domain suffix (e.g., "example.com").
	// +kubebuilder:validation:MinLength=1
	Domain string `json:"domain"`
}

// AccessListRef references a Cloudflare Access list by ID or name.
type AccessListRef struct {
	// ID of the Access list in Cloudflare.
	// +optional
	ID string `json:"id,omitempty"`

	// Name of the Access list (looked up via API).
	// +optional
	Name string `json:"name,omitempty"`
}

// IPRangeRule matches source IP CIDR ranges.
type IPRangeRule struct {
	// Ranges are CIDR blocks.
	// +kubebuilder:validation:MinItems=1
	Ranges []string `json:"ranges"`
}

// CountryRule matches source country codes.
type CountryRule struct {
	// Codes are ISO 3166-1 alpha-2 country codes.
	// +kubebuilder:validation:MinItems=1
	Codes []string `json:"codes"`
}

// CommonNameRule matches certificate common name.
type CommonNameRule struct {
	// Value is the expected common name.
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// AccessGroupRef references an AccessGroup CR or Cloudflare group.
type AccessGroupRef struct {
	// Name of AccessGroup CR in same namespace.
	// +optional
	Name string `json:"name,omitempty"`

	// CloudflareID of group in Cloudflare (bypasses CR lookup).
	// +optional
	CloudflareID string `json:"cloudflareId,omitempty"`
}

// GSuiteRule matches Google Workspace groups.
type GSuiteRule struct {
	// IdentityProviderID in Cloudflare.
	// +kubebuilder:validation:MinLength=1
	IdentityProviderID string `json:"identityProviderId"`

	// Groups to match.
	// +optional
	Groups []string `json:"groups,omitempty"`
}

// GitHubRule matches GitHub organization membership.
type GitHubRule struct {
	// IdentityProviderID in Cloudflare.
	// +kubebuilder:validation:MinLength=1
	IdentityProviderID string `json:"identityProviderId"`

	// Organization name.
	// +optional
	Organization string `json:"organization,omitempty"`

	// Teams within organization.
	// +optional
	Teams []string `json:"teams,omitempty"`
}

// AzureRule matches Azure AD groups.
type AzureRule struct {
	// IdentityProviderID in Cloudflare.
	// +kubebuilder:validation:MinLength=1
	IdentityProviderID string `json:"identityProviderId"`

	// Groups are Azure AD group IDs.
	// +optional
	Groups []string `json:"groups,omitempty"`
}

// OktaRule matches Okta groups.
type OktaRule struct {
	// IdentityProviderID in Cloudflare.
	// +kubebuilder:validation:MinLength=1
	IdentityProviderID string `json:"identityProviderId"`

	// Groups to match.
	// +optional
	Groups []string `json:"groups,omitempty"`
}

// SAMLRule matches SAML assertion attributes.
type SAMLRule struct {
	// IdentityProviderID in Cloudflare.
	// +kubebuilder:validation:MinLength=1
	IdentityProviderID string `json:"identityProviderId"`

	// AttributeName to match.
	// +kubebuilder:validation:MinLength=1
	AttributeName string `json:"attributeName"`

	// AttributeValue expected.
	// +kubebuilder:validation:MinLength=1
	AttributeValue string `json:"attributeValue"`
}

// ApprovalGroup defines who can approve access requests.
type ApprovalGroup struct {
	// Emails of approvers.
	// +optional
	Emails []string `json:"emails,omitempty"`

	// EmailDomain allows any user from domain to approve.
	// +optional
	EmailDomain string `json:"emailDomain,omitempty"`

	// ApprovalsNeeded is number of approvals required.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	ApprovalsNeeded int `json:"approvalsNeeded,omitempty"`
}

// ServiceTokenConfig defines machine-to-machine authentication.
type ServiceTokenConfig struct {
	// Name is the token display name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Duration is the token validity period.
	// +kubebuilder:validation:Pattern=`^[0-9]+(h|d|y)$`
	// +kubebuilder:default="365d"
	Duration string `json:"duration,omitempty"`

	// SecretRef stores the generated token credentials.
	// +kubebuilder:validation:Required
	SecretRef ServiceTokenSecretRef `json:"secretRef"`
}

// ServiceTokenSecretRef references a Kubernetes Secret for service token storage.
type ServiceTokenSecretRef struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// MTLSConfig defines certificate-based authentication.
type MTLSConfig struct {
	// Enabled activates mTLS requirement.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// RootCASecretRef references the CA certificate(s) for validation.
	// +optional
	RootCASecretRef *CASecretRef `json:"rootCaSecretRef,omitempty"`

	// AssociatedHostnames limits mTLS to specific hostnames.
	// +optional
	AssociatedHostnames []string `json:"associatedHostnames,omitempty"`

	// RuleName is the name of the mTLS rule in Cloudflare.
	// Defaults to CR name if omitted.
	// +optional
	RuleName string `json:"ruleName,omitempty"`
}

// CASecretRef references a CA certificate Secret.
type CASecretRef struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key within Secret (defaults to ca.crt).
	// +kubebuilder:default="ca.crt"
	Key string `json:"key,omitempty"`
}

// CloudflareAccessPolicySpec defines the desired state of CloudflareAccessPolicy.
// +kubebuilder:validation:XValidation:rule="has(self.targetRef) || has(self.targetRefs)",message="either targetRef or targetRefs must be specified"
// +kubebuilder:validation:XValidation:rule="!(has(self.targetRef) && has(self.targetRefs))",message="targetRef and targetRefs are mutually exclusive"
type CloudflareAccessPolicySpec struct {
	// TargetRef identifies a single target for policy attachment.
	// +optional
	TargetRef *PolicyTargetReference `json:"targetRef,omitempty"`

	// TargetRefs identifies multiple targets for policy attachment.
	// +optional
	TargetRefs []PolicyTargetReference `json:"targetRefs,omitempty"`

	// CloudflareRef references Cloudflare credentials (inherits from tunnel if omitted).
	// +optional
	CloudflareRef *CloudflareSecretRef `json:"cloudflareRef,omitempty"`

	// Application defines the Access Application settings.
	Application AccessApplication `json:"application"`

	// Policies define access rules (evaluated in order).
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Policies []AccessPolicyRule `json:"policies,omitempty"`

	// GroupRefs reference reusable identity rules.
	// +optional
	GroupRefs []AccessGroupRef `json:"groupRefs,omitempty"`

	// ServiceTokens for machine-to-machine authentication.
	// +optional
	ServiceTokens []ServiceTokenConfig `json:"serviceTokens,omitempty"`

	// MTLS configures certificate-based authentication.
	// +optional
	MTLS *MTLSConfig `json:"mtls,omitempty"`
}

// PolicyAncestorStatus describes attachment status per target.
// Follows Gateway API PolicyAncestorStatus pattern.
type PolicyAncestorStatus struct {
	// AncestorRef identifies the target.
	AncestorRef PolicyTargetReference `json:"ancestorRef"`

	// ControllerName identifies the controller managing this attachment.
	ControllerName string `json:"controllerName"`

	// Conditions for this specific target.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CloudflareAccessPolicyStatus defines the observed state of CloudflareAccessPolicy.
type CloudflareAccessPolicyStatus struct {
	// ApplicationID is the Cloudflare Access Application ID.
	ApplicationID string `json:"applicationId,omitempty"`

	// ApplicationAUD is the Application Audience Tag.
	ApplicationAUD string `json:"applicationAud,omitempty"`

	// AttachedTargets is the count of successfully attached targets.
	AttachedTargets int32 `json:"attachedTargets,omitempty"`

	// ServiceTokenIDs maps token names to Cloudflare IDs.
	ServiceTokenIDs map[string]string `json:"serviceTokenIds,omitempty"`

	// MTLSRuleID is the Cloudflare mTLS rule ID.
	MTLSRuleID string `json:"mtlsRuleId,omitempty"`

	// ObservedGeneration is the last generation processed.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions describe current state.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Ancestors contains status for each targetRef (Gateway API PolicyStatus).
	// +optional
	Ancestors []PolicyAncestorStatus `json:"ancestors,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=cfap;cfaccess
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Application",type="string",JSONPath=".status.applicationId"
// +kubebuilder:printcolumn:name="Targets",type="integer",JSONPath=".status.attachedTargets"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// CloudflareAccessPolicy is the Schema for the cloudflareaccespolicies API.
// It manages Cloudflare Access Applications and Policies for zero-trust access control.
type CloudflareAccessPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudflareAccessPolicySpec   `json:"spec,omitempty"`
	Status CloudflareAccessPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudflareAccessPolicyList contains a list of CloudflareAccessPolicy.
type CloudflareAccessPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudflareAccessPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudflareAccessPolicy{}, &CloudflareAccessPolicyList{})
}
