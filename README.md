# cfgate Helm Chart

[![Chart Version](https://img.shields.io/github/v/release/cfgate/helm-chart?style=flat&label=chart&logo=helm&logoColor=white&color=0F1689)](https://github.com/cfgate/helm-chart/releases/latest) [![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cfgate-helm)](https://artifacthub.io/packages/helm/helm-chart-cfgate/cfgate)

[![CI](https://img.shields.io/github/actions/workflow/status/cfgate/helm-chart/ci.yml?style=flat)](https://github.com/cfgate/helm-chart/actions/workflows/ci.yml) [![License](https://img.shields.io/github/license/cfgate/helm-chart?style=flat)](LICENSE)

Installs the cfgate controller, a Gateway API-native Kubernetes operator for Cloudflare Tunnel, DNS, and Access management.

This chart targets the next cfgate release, `0.2.0-alpha.6`. Chart `1.5.0` must be published after that controller image is available.

The current cfgate surface managed by this chart includes separate `CloudflareAccessApplication` and `CloudflareAccessPolicy` CRDs for Access application and policy lifecycle management.

The chart deploys:
- Controller Deployment (with health probes, security context, resource limits)
- CRDs (CloudflareTunnel, CloudflareDNS, CloudflareAccessApplication, CloudflareAccessPolicy)
- ClusterRole and ClusterRoleBinding
- ServiceAccount
- Metrics Service (optional ServiceMonitor for Prometheus)

## Prerequisites

- Kubernetes compatible with the installed Gateway API bundle. The upstream standard bundle used below requires Kubernetes 1.30 or later; this API minimum is not a cfgate-tested support range.
- Helm 3.x or 4.x
- Gateway API CRDs installed:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.2/standard-install.yaml
```

Gateway API CRDs are a cluster-level prerequisite, not a chart dependency. They may already be installed if you run Istio, Cilium, Envoy Gateway, or another Gateway API implementation.

The chart's historical `kubeVersion: ">= 1.26.0-0"` controls Helm admission for its own templates; it does not certify the complete controller and Gateway API stack on Kubernetes 1.26. Upstream [Gateway API installation requirements](https://kubernetes.io/blog/2026/04/21/gateway-api-v1-5/) and cfgate's release validation are separate checks. The final chart release must record the exact Kubernetes, Gateway API, controller image, and CRD versions actually tested.

## Install

```bash
helm install cfgate oci://ghcr.io/cfgate/charts/cfgate \
  --namespace cfgate-system --create-namespace
```

## Upgrade

```bash
helm upgrade cfgate oci://ghcr.io/cfgate/charts/cfgate \
  --namespace cfgate-system
```

### Upgrade from 1.4.0 to 1.5.0

Chart 1.4.0 → 1.5.0 upgrades cfgate `0.2.0-alpha.5` → `0.2.0-alpha.6`. The chart keeps existing values and template names compatible while adding controller configuration and CRD fields. The controller's stricter ownership and authorization rules require migration for existing resources that relied on implicit adoption or unrestricted cross-namespace references. This is not an unattended image-only upgrade.

Before rollout:

1. Inventory existing CR UIDs, connector owner references, remote tunnel IDs, DNS data and TXT ownership records. Stop previous writers for resources being migrated and preserve the installation namespace: its Kubernetes UID now forms part of persistent ownership.
2. Add explicit, narrowly scoped ReferenceGrants for cross-namespace credentials, Gateway/DNS/Access-to-Tunnel references, and HTTPRoute backends. Same-namespace references remain a namespace trust boundary. Keep infrastructure CRs, credential Secrets, ownership claims, connector images, and arguments under administrator control.
3. Verify generated connector resources and Access service-token Secrets have their expected controller owner UID. cfgate refuses to overwrite unowned or foreign objects. Deliberately migrate or recreate those objects after inspecting their current owner.
4. For an existing, verified, unclaimed remote tunnel, temporarily opt in with `cfgate.io/adopt-existing: "true"`. A foreign ownership claim still blocks adoption. Remove the opt-in after successful migration.
5. Migrate legacy DNS ownership deliberately after stopping the previous writer. `spec.ownership.ownerId` and `cleanupPolicy.onlyManaged: false` no longer authorize overwriting or deleting another owner's records. Inspect legacy TXT claims and data markers before any manual transition; never remove a claim while its owner is active.
6. Install the matching four CRD schemas before running the new controller, including when `installCRDs=false`. New status fields retain credential selection, installation ownership, and reconciliation state; old schemas must not prune them.
7. Verify remote configuration, connector readiness, DNS ownership, and application requests before resuming normal traffic. Review the supported HTTPRoute subset: unsupported restrictions are rejected, while invalid attached backends return matching failure responses instead of falling through to broader routes.

See the [controller authorization and migration guide](https://github.com/cfgate/cfgate/blob/v0.2.0-alpha.6/docs/authorization-and-ownership.md) for exact grants, ownership rules, and coordination limits. Chart 1.5.0 remains downstream of the approved controller release; do not publish it before that image and its matching schemas are available.

Controller Pods disable Service environment injection (`enableServiceLinks: false`), preventing the metrics Service from injecting a conflicting `CFGATE_METRICS_PORT` URL. Existing explicit metrics and health flags remain supported. DNS CRD descriptions preserve the literal `{{ .TunnelDomain }}` variable. CRD schemas and RBAC must be synchronized with the final controller release; they are no longer unchanged from chart 1.4.0.

### Candidate provenance and validation limits

The four rendered CRD specifications and manager RBAC must match the controller source being released. Repeat the normalized comparisons after code generation or dependency updates; record that source commit and the released controller image digest with release evidence. The bundled connector default is `ghcr.io/inherent-design/cloudflared:2026.9.3-h2c.1`.

Helm lint, local rendering, JSON Schema validation, and normalized CRD/RBAC comparisons verify chart generation. They do not establish a tested Kubernetes range, an actual cluster upgrade, or live Cloudflare compatibility. Record exact cluster, external Gateway bundle, chart, controller, connector and CRD versions from final release tests before publishing.

For installations older than chart 1.4.0, first follow the [historical 1.4.0 migration notes](https://github.com/cfgate/helm-chart/blob/v1.4.0/README.md#upgrade-from-131-to-140), then apply the upgrade above.

## Uninstall

```bash
helm uninstall cfgate --namespace cfgate-system
```

CRDs are not deleted on uninstall (annotated with `helm.sh/resource-policy: keep`). This prevents accidental deletion of all CloudflareTunnel, CloudflareDNS, CloudflareAccessApplication, and CloudflareAccessPolicy resources in the cluster. To remove CRDs manually:

```bash
kubectl delete crd cloudflaretunnels.cfgate.io
kubectl delete crd cloudflarednses.cfgate.io
kubectl delete crd cloudflareaccessapplications.cfgate.io
kubectl delete crd cloudflareaccesspolicies.cfgate.io
```

## Configuration

### Controller

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `replicaCount` | int | `2` | Number of controller replicas |
| `image.repository` | string | `ghcr.io/cfgate/cfgate` | Container image repository |
| `image.tag` | string | Chart appVersion | Container image tag |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | list | `[]` | Image pull secrets |
| `nameOverride` | string | `""` | Override chart name |
| `fullnameOverride` | string | `""` | Override full release name |
| `namespaceOverride` | string | `""` | Override release namespace |

### Controller limits and installation identity

| Key | Type | Default | Manager flag |
|-----|------|---------|--------------|
| `controller.clusterDomain` | string | `cluster.local` | `--cluster-domain` |
| `controller.installationNamespace` | string | `""` (actual Pod namespace) | `--installation-namespace`, when set |
| `controller.cloudflareRequestTimeoutSeconds` | int | `30` | `--cloudflare-request-timeout=30s` |
| `controller.maxIngressRules` | int | `1000` | `--max-ingress-rules` |
| `controller.maxConfigurationBytes` | int | `1048576` | `--max-configuration-bytes` |

Timeout values are positive whole seconds, with a maximum of 9223372036 seconds (the manager's duration representation limit). Rule and byte limits accept positive integers up to 2147483647. These are operator work limits, not Cloudflare service limits; exceeding them rejects configuration publication instead of truncating routes. The rule budget includes the fallback rule. The byte budget includes serialized ingress and origin settings.

`clusterDomain` accepts a lowercase DNS suffix without a trailing dot. Use the suffix actually configured in the cluster; setting the value does not reconfigure cluster DNS. For example:

```yaml
controller:
  clusterDomain: cluster.internal
  cloudflareRequestTimeoutSeconds: 20
  maxIngressRules: 2000
  maxConfigurationBytes: 2097152
```

`POD_NAMESPACE` comes from the Pod's actual `metadata.namespace`, so `namespaceOverride` also selects the default installation namespace correctly. The namespace's Kubernetes UID and each resource UID participate in ownership. Preserve that namespace across upgrades. Setting `controller.installationNamespace` selects an existing namespace for identity and tunnel claims; it does not create one. Changing that value, deleting/recreating the namespace, or recreating owned CRs requires the documented ownership migration.

### CRDs and RBAC

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `installCRDs` | bool | `true` | Install CRDs with the chart |
| `rbac.create` | bool | `true` | Create ClusterRole and ClusterRoleBinding |
| `serviceAccount.create` | bool | `true` | Create ServiceAccount |
| `serviceAccount.name` | string | `""` | ServiceAccount name (generated if empty) |
| `serviceAccount.annotations` | object | `{}` | ServiceAccount annotations |

The manager ClusterRole includes `get/create/delete` on ConfigMaps for immutable installation tunnel claims, and `get/list/watch` on Pods for connector drain checks. These permissions are part of the matching controller RBAC; when `rbac.create=false`, provide equivalent administrator-managed rules. Do not disable the manager's service-account token: it needs Kubernetes API access. Connector Pods independently disable unused token mounting.

Access-required routing remains an explicit per-HTTPRoute opt-in (`cfgate.io/access-required: namespace/name`), not a chart-wide setting. Its dependency receipts are included in the Tunnel CRD. The selected tunnel credential needs Access application/policy read permissions. See the [Access-required contract and limits](https://github.com/cfgate/cfgate/blob/v0.2.0-alpha.6/docs/access-required.md); edge configuration is asynchronous, and strict or gRPC authentication requires origin-side enforcement.

### Metrics and Monitoring

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `metrics.port` | int | `8080` | Metrics endpoint port |
| `metrics.service.enabled` | bool | `true` | Create metrics Service |
| `metrics.service.port` | int | `8080` | Metrics Service port |
| `metrics.service.annotations` | object | `{}` | Metrics Service annotations |
| `metrics.serviceMonitor.enabled` | bool | `false` | Create Prometheus ServiceMonitor |
| `metrics.serviceMonitor.namespace` | string | `""` | ServiceMonitor namespace (defaults to release namespace) |
| `metrics.serviceMonitor.interval` | string | `30s` | Scrape interval |
| `metrics.serviceMonitor.labels` | object | `{}` | Additional ServiceMonitor labels |

### Health Probes

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `health.port` | int | `8081` | Health probe port (`/healthz`, `/readyz`) |

### Pod Scheduling

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `resources.requests.cpu` | string | `100m` | CPU request |
| `resources.requests.memory` | string | `128Mi` | Memory request |
| `resources.limits.cpu` | string | `500m` | CPU limit |
| `resources.limits.memory` | string | `256Mi` | Memory limit |
| `nodeSelector` | object | `{}` | Node selector |
| `tolerations` | list | `[]` | Tolerations |
| `affinity` | object | `{}` | Affinity rules |
| `podAnnotations` | object | `{}` | Pod annotations |
| `podLabels` | object | `{}` | Pod labels |

### Security

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `securityContext.allowPrivilegeEscalation` | bool | `false` | Disallow privilege escalation |
| `securityContext.capabilities.drop` | list | `[ALL]` | Drop all capabilities |
| `securityContext.readOnlyRootFilesystem` | bool | `true` | Read-only root filesystem |
| `podSecurityContext.runAsNonRoot` | bool | `true` | Run as non-root |
| `podSecurityContext.seccompProfile.type` | string | `RuntimeDefault` | Seccomp profile |

## High Availability

High availability is enabled by default with 2 replicas. To scale further:

```yaml
replicaCount: 3
```

Leader election is always enabled via `--leader-elect`, so only one replica actively reconciles at a time while standby replicas take over if the leader fails.

## Monitoring

### Prometheus ServiceMonitor

If you run the Prometheus Operator:

```yaml
metrics:
  serviceMonitor:
    enabled: true
    labels:
      release: prometheus  # match your Prometheus selector
```

### Prometheus Annotations

If you use annotation-based scraping instead of ServiceMonitor:

```yaml
metrics:
  service:
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/port: "8080"
```

## Next Steps

After installing the chart, see the [cfgate documentation](https://github.com/cfgate/cfgate) for:
- Creating CloudflareTunnel, CloudflareDNS, CloudflareAccessApplication, and CloudflareAccessPolicy resources
- Setting up Gateway API GatewayClass and Gateway
- Configuring HTTPRoute annotations for per-route origin settings
- Multi-zone DNS configuration

## Artifact Hub

This chart is published to [Artifact Hub](https://artifacthub.io/) via OCI at `oci://ghcr.io/cfgate/charts/cfgate`.

### Prerelease Annotations

Chart.yaml includes Artifact Hub annotations that control how the chart appears in search results:

| Annotation | Value | Purpose |
|-----------|-------|---------|
| `artifacthub.io/prerelease` | `"true"` or `"false"` | Marks chart as prerelease in Artifact Hub UI |
| `artifacthub.io/license` | `Apache-2.0` | SPDX license identifier |
| `artifacthub.io/category` | `networking` | Artifact Hub category filter |
| `artifacthub.io/operator` | `"true"` | Flags chart as a Kubernetes operator |
| `artifacthub.io/operatorCapabilities` | `Basic Install` | Operator maturity level |
| `artifacthub.io/images` | (YAML list) | Container images used by the chart |

### Release Transitions

When moving between release stages, update `Chart.yaml` annotations:

**Alpha/Beta builds** (`appVersion: "0.0.0-alpha.1"`):
```yaml
artifacthub.io/prerelease: "true"
```

**Release candidates** (`appVersion: "0.0.0-rc.1"`):
```yaml
artifacthub.io/prerelease: "true"
```

**Stable releases** (`appVersion: "0.0.0"`):
```yaml
artifacthub.io/prerelease: "false"
```

Update `Chart.yaml` manually before tagging:
- set `version` to match the release tag without the leading `v`
- set `appVersion` to the cfgate version the chart targets
- set `artifacthub.io/prerelease` appropriately for prerelease vs stable publication

The release workflow validates that the tag matches `Chart.yaml version`, but it does not rewrite chart metadata for you.

## Chart Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for chart-project synchronization, CRD regeneration, and RBAC updates.
