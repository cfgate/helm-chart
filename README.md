# cfgate Helm Chart

[![Chart Version](https://img.shields.io/github/v/release/cfgate/helm-chart?style=flat&label=chart&logo=helm&logoColor=white&color=0F1689)](https://github.com/cfgate/helm-chart/releases/latest) [![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/cfgate-helm)](https://artifacthub.io/packages/helm/helm-chart-cfgate/cfgate)

[![CI](https://img.shields.io/github/actions/workflow/status/cfgate/helm-chart/ci.yml?style=flat)](https://github.com/cfgate/helm-chart/actions/workflows/ci.yml) [![License](https://img.shields.io/github/license/cfgate/helm-chart?style=flat)](LICENSE)

Installs the cfgate controller, a Gateway API-native Kubernetes operator for Cloudflare Tunnel, DNS, and Access management.

Chart `1.9.0` installs cfgate [`0.2.0-alpha.10`](https://github.com/cfgate/cfgate/releases/tag/v0.2.0-alpha.10).

The chart deploys:
- Controller Deployment (with health probes, security context, resource limits)
- CRDs (CloudflareTunnel, CloudflareDNS, CloudflareAccessApplication, CloudflareAccessPolicy)
- Manager ClusterRole/Binding and namespaced claim Role/Binding
- ServiceAccount
- Metrics Service (optional ServiceMonitor for Prometheus)

## Prerequisites

- Kubernetes 1.30 or later for the Gateway API bundle below.
- Helm 3.x or 4.x
- Gateway API CRDs installed:

```bash
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.2/standard-install.yaml
```

Gateway API CRDs are a cluster-level prerequisite, not a chart dependency. They may already be installed if you run Istio, Cilium, Envoy Gateway, or another Gateway API implementation.

The chart's `kubeVersion` floor applies to its templates. The installed Gateway API bundle may require a newer Kubernetes version, as shown above.

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

### Upgrade from 1.8.0 to 1.9.0

This upgrades cfgate `0.2.0-alpha.9` to `0.2.0-alpha.10`. Existing chart values
and CRD fields remain compatible. Clear or update explicit image overrides to
select the new chart-owned digest.

- explicit `cfgate.io/origin-ssl-verify: "true"` now overrides an insecure tunnel default; check origin certificates and CA bundles before upgrading
- origin protocol and documented Boolean aliases are case-insensitive; invalid transport annotations are rejected
- explicit `origin-http2: "false"` and `origin-h2c: "false"` now disable inherited settings; disable the inherited transport when switching between them
- origin connect timeouts must represent positive whole seconds, such as `10s` or `1m`
- omitted route `cfgate.io/ttl` now inherits the DNS resource's default; set `"1"` to keep Auto under a custom default
- duplicate hostname settings are compared after inheritance and proxied-TTL normalization; genuinely different effective records still conflict

Preserve installation identity and pending recovery state through the upgrade.
Resolve pending DNS writes and service-token distribution before any rollback.
For older installations, follow the versioned
[1.7.0 to 1.8.0 migration notes](https://github.com/cfgate/helm-chart/blob/v1.8.0/README.md#upgrade-from-170-to-180)
first, including selector validation and stored TTL overrides.

## Controller removal

For a temporary controller-only removal:

```bash
helm uninstall cfgate --namespace cfgate-system
```

The chart retains CRDs. Keep custom resources, credential Secrets, ReferenceGrants,
and the installation namespace with its ownership claims so the matching controller
can resume work after reinstallation. Connector Pods and Cloudflare resources can
keep serving existing traffic. Configuration updates, drift repair, token renewal
and finalization stop while the controller is absent.

## Full decommissioning

Keep the controller, its RBAC, credentials and grants running through cleanup.
Inventory the resources belonging to this installation and check retention and
orphan policies before deleting anything.

1. Remove its routes or attachments and confirm tunnel publication has withdrawn
   their forwarding. Kubernetes deletion alone does not prove remote withdrawal.
2. Delete its CloudflareDNS and CloudflareAccessApplication objects. Wait for their
   finalizers while referenced tunnels and policies remain available.
3. Delete its CloudflareAccessPolicy objects and wait for policy/token cleanup.
   Then delete its CloudflareTunnel objects and wait for connector drain and remote
   tunnel cleanup.
4. Verify remote resources are removed or deliberately retained with an owner
   handoff. Only then uninstall the chart and remove unused credentials and claims.
5. Delete CRDs only when no installation still uses them. Deleting a CRD affects
   every object of that kind in the cluster.

Select explicit names and namespaces. A chart release does not own every cfgate
object in the cluster. If deletion stalls, inspect conditions, events and logs;
retain cleanup credentials and grants. Removing finalizers or recovery status
bypasses cleanup and can leave Cloudflare resources behind.

## Configuration

### Controller

The manager requests up to 30 seconds for graceful shutdown. Kubernetes can force
termination when the Pod allowance expires; a longer allowance does not replace
recovery after interrupted writes. Credentials stored in Secrets also need to be
reloaded by their consumers independently.

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `replicaCount` | int | `2` | Number of controller replicas |
| `terminationGracePeriodSeconds` | int | `30` | Pod shutdown allowance; `0` requests immediate termination |
| `image.repository` | string | `ghcr.io/cfgate/cfgate` | Container image repository |
| `image.tag` | string | `""` | Explicit tag opts out of the chart's default digest pin |
| `image.digest` | string | `""` | Explicit SHA-256 digest; takes precedence over tag |
| `image.pullPolicy` | string | `IfNotPresent` | Image pull policy |
| `imagePullSecrets` | list | `[]` | Image pull secrets |
| `nameOverride` | string | `""` | Override chart name |
| `fullnameOverride` | string | `""` | Override full release name |
| `namespaceOverride` | string | `""` | Override release namespace |

With the default repository and empty `image.tag`/`image.digest`, the Deployment
uses the verified multi-architecture digest recorded in
`Chart.yaml`'s `cfgate.io/operator-image-digest` annotation. This chart-owned pin
updates with the chart even when upgrading with `--reuse-values`.

An explicit tag or custom repository preserves tag-based selection unless
`image.digest` is supplied. A custom repository with an empty tag uses
`appVersion`. Custom images are administrator choices and are not certified by
the chart's release guard. Set `image.digest: sha256:<64 lowercase hex characters>`
to pin a custom image; that digest takes precedence over any tag. An explicitly
set digest persists with reused values, so update or clear it during upgrades.

### Controller limits and installation identity

| Key | Type | Default | Manager flag |
|-----|------|---------|--------------|
| `controller.clusterDomain` | string | `cluster.local` | `--cluster-domain` |
| `controller.installationNamespace` | string | `""` (actual Pod namespace) | `--installation-namespace`, when set |
| `controller.cloudflareRequestTimeoutSeconds` | int | `30` | `--cloudflare-request-timeout=30s` |
| `controller.maxIngressRules` | int | `1000` | `--max-ingress-rules` |
| `controller.maxConfigurationBytes` | int | `1048576` | `--max-configuration-bytes` |

Timeout values are positive whole seconds, with a maximum of 9223372036 seconds (the manager's duration representation limit). Rule limits accept integers from 1 to 2147483647; byte limits accept 67 to 2147483647 so the controller can publish an emergency denial. These are operator work limits, not Cloudflare service limits; exceeding them replaces tunnel forwarding with HTTP 503 until the configuration fits. The rule budget includes the fallback rule. The byte budget includes serialized ingress and origin settings.

`clusterDomain` accepts a lowercase DNS suffix without a trailing dot. Use the suffix actually configured in the cluster; setting the value does not reconfigure cluster DNS. For example:

```yaml
controller:
  clusterDomain: cluster.internal
  cloudflareRequestTimeoutSeconds: 20
  maxIngressRules: 2000
  maxConfigurationBytes: 2097152
```

`POD_NAMESPACE` comes from the Pod's actual `metadata.namespace`, so `namespaceOverride` also selects the default installation namespace correctly. The namespace's Kubernetes UID and each resource UID participate in ownership. Preserve that namespace across upgrades. Setting `controller.installationNamespace` selects an existing namespace for identity and Tunnel/Access claims; it does not create one. Changing that value, deleting/recreating the namespace, or recreating owned CRs requires the documented ownership migration.

### CRDs and RBAC

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `installCRDs` | bool | `true` | Install CRDs with the chart |
| `rbac.create` | bool | `true` | Create manager ClusterRole/Binding and installation claim Role/Binding |
| `serviceAccount.create` | bool | `true` | Create ServiceAccount |
| `serviceAccount.name` | string | `""` | ServiceAccount name (generated if empty) |
| `serviceAccount.annotations` | object | `{}` | ServiceAccount annotations |

The manager's namespaced claim Role grants `get/create/delete` on ConfigMaps in
`controller.installationNamespace`, or the actual manager namespace when unset.
Its RoleBinding targets the manager's service account, even when the claim
namespace differs. A custom installation namespace must already exist. The
ClusterRole retains `get/list/watch` on Pods for connector drain checks, but
grants no ConfigMap access.

When `rbac.create=false`, supply both the cluster-wide manager permissions and
the namespaced claim permissions yourself. Remove any previous cluster-wide
ConfigMap grant; adding a namespaced Role alone does not revoke it. This scopes
ConfigMap access to a namespace, not individual claims, and leaves other required
manager permissions unchanged. Do not disable the manager's service-account token: it
needs Kubernetes API access. Connector Pods independently disable unused token
mounting.

Access-required routing remains an explicit per-HTTPRoute opt-in (`cfgate.io/access-required: namespace/name`), not a chart-wide setting. Its dependency receipts are included in the Tunnel CRD. The selected tunnel credential needs Access application/policy read permissions. See the [Access-required contract and limits](https://github.com/cfgate/cfgate/blob/v0.2.0-alpha.7/docs/access-required.md); edge configuration is asynchronous, and strict or gRPC authentication requires origin-side enforcement.

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

The chart defaults to two controller replicas. To scale further:

```yaml
replicaCount: 3
```

Leader election is always enabled via `--leader-elect`, so only one replica actively reconciles at a time while standby replicas take over if the leader fails. Replicas alone do not ensure placement on different nodes; configure `affinity` for the failure domains your deployment requires.

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

The chart is published at `oci://ghcr.io/cfgate/charts/cfgate` and listed on
[Artifact Hub](https://artifacthub.io/packages/helm/helm-chart-cfgate/cfgate).

## Chart Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for chart-project synchronization, CRD regeneration, and RBAC updates.
