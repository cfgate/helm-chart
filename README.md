# cfgate Helm Chart

[![Chart Version](https://img.shields.io/github/v/release/cfgate/helm-chart?style=flat&label=chart&logo=helm&logoColor=white&color=0F1689)](https://github.com/cfgate/helm-chart/releases/latest) [![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cfgate-helm)](https://artifacthub.io/packages/helm/helm-chart-cfgate/cfgate)

[![CI](https://img.shields.io/github/actions/workflow/status/cfgate/helm-chart/ci.yml?style=flat)](https://github.com/cfgate/helm-chart/actions/workflows/ci.yml) [![License](https://img.shields.io/github/license/cfgate/helm-chart?style=flat)](LICENSE)

Installs the cfgate controller, a Gateway API-native Kubernetes operator for Cloudflare Tunnel, DNS, and Access management.

Chart `1.5.0` installs cfgate [`0.2.0-alpha.6`](https://github.com/cfgate/cfgate/releases/tag/v0.2.0-alpha.6).

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

The chart's historical `kubeVersion: ">= 1.26.0-0"` controls Helm admission for its own templates; it does not certify the complete controller and Gateway API stack on Kubernetes 1.26. Upstream [Gateway API installation requirements](https://kubernetes.io/blog/2026/04/21/gateway-api-v1-5/) and the [tested configuration below](#release-validation) are separate checks.

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

Chart 1.4.0 → 1.5.0 upgrades cfgate `0.2.0-alpha.5` → `0.2.0-alpha.6`. Existing chart values remain compatible.

- no values change is needed for #85; the chart disables Service environment injection and the controller fixes configuration precedence
- if `image.tag` is explicitly set, update it to `0.2.0-alpha.6`; an empty tag follows the chart's appVersion
- if `installCRDs=false` or `rbac.create=false`, update the externally managed CRDs and RBAC to alpha.6 before rollout
- cross-namespace references now require the applicable `ReferenceGrant`
- existing tunnels, DNS records, and generated resources must satisfy stricter ownership checks; preserve the installation namespace and follow the migration guide before adopting legacy resources
- to hold routes closed until Access is ready, set `cfgate.io/access-required`; intentionally public routes need no change

See the [alpha.5 → alpha.6 migration guide](https://github.com/cfgate/cfgate/blob/v0.2.0-alpha.6/docs/authorization-and-ownership.md#upgrade-from-v020-alpha5-to-v020-alpha6) for adoption, DNS ownership, and authorization details.

### Release validation

The four rendered CRD specifications and manager RBAC match released cfgate source `b8cb740`; the CRD specifications also match its published release assets. The published image's GitHub provenance verifies against `v0.2.0-alpha.6` and that source commit. The bundled connector default is `ghcr.io/inherent-design/cloudflared:2026.9.3-h2c.1`.

A disposable ARM64 kind cluster running Kubernetes 1.37.0 and Gateway API 1.6.2 passed a chart 1.4.0 → 1.5.0 upgrade using Helm 4.3.0 and `--reuse-values`. The old controller reproduced #85; the published alpha.6 image became Ready, preserved custom metrics/health ports, and passed a rollout restart. Installed CRD schemas, origin-setting validation, and the manager's service-account token were checked.

The tested operator index digest was `sha256:3d3eaeae0ae0a76f8b3bc5271f642cf06f42e6525c85cb16e1b778fb4d864d6a`. This chart check created no Cloudflare resources and does not establish a Kubernetes support range. The controller's separate [release run](https://github.com/cfgate/cfgate/actions/runs/36822793906) passed all 119 live E2E tests and both architecture scans.

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
