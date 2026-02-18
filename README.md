# cfgate Helm Chart

Installs the cfgate controller -- a Gateway API-native Kubernetes operator for Cloudflare Tunnel, DNS, and Access management.

The chart deploys:
- Controller Deployment (with health probes, security context, resource limits)
- CRDs (CloudflareTunnel, CloudflareDNS, CloudflareAccessPolicy)
- ClusterRole and ClusterRoleBinding
- ServiceAccount
- Metrics Service (optional ServiceMonitor for Prometheus)

## Prerequisites

- Kubernetes 1.26+
- Helm 3.x
- Gateway API CRDs installed:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml
```

Gateway API CRDs are a cluster-level prerequisite, not a chart dependency. They may already be installed if you run Istio, Cilium, Envoy Gateway, or another Gateway API implementation.

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

## Uninstall

```bash
helm uninstall cfgate --namespace cfgate-system
```

CRDs are **not** deleted on uninstall (annotated with `helm.sh/resource-policy: keep`). This prevents accidental deletion of all CloudflareTunnel, CloudflareDNS, and CloudflareAccessPolicy resources in the cluster. To remove CRDs manually:

```bash
kubectl delete crd cloudflaretunnels.cfgate.io
kubectl delete crd cloudflarednses.cfgate.io
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

### CRDs and RBAC

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `installCRDs` | bool | `true` | Install CRDs with the chart |
| `rbac.create` | bool | `true` | Create ClusterRole and ClusterRoleBinding |
| `serviceAccount.create` | bool | `true` | Create ServiceAccount |
| `serviceAccount.name` | string | `""` | ServiceAccount name (generated if empty) |
| `serviceAccount.annotations` | object | `{}` | ServiceAccount annotations |

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

HA is enabled by default with 2 replicas. To scale further:

```yaml
replicaCount: 3
```

Leader election is always enabled (`--leader-elect`). Only one replica actively reconciles at a time. Standby replicas take over if the leader fails.

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
- Creating CloudflareTunnel, CloudflareDNS, and CloudflareAccessPolicy resources
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

**Alpha/Beta builds** (`appVersion: "0.1.0-alpha.10"`):
```yaml
artifacthub.io/prerelease: "true"
```

**Release candidates** (`appVersion: "0.1.0-rc.1"`):
```yaml
artifacthub.io/prerelease: "true"
```

**Stable releases** (`appVersion: "0.1.0"`):
```yaml
artifacthub.io/prerelease: "false"
```

The `appVersion` in Chart.yaml is auto-synced during release (see `.github/workflows/release.yml`). The `artifacthub.io/prerelease` annotation must be updated manually before tagging a stable release.

## Chart Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for chart-project synchronization, CRD regeneration, and RBAC updates.
