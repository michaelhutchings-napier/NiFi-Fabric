# Compatibility

NiFi-Fabric targets Apache NiFi `2.0.x` through `2.8.x`.

The supported `2.x` line uses one common feature set. Focused repository runtime anchors stay bounded at `2.0.0` and `2.8.0`, and the broader `2.0.x` through `2.8.x` support claim follows the same feature set plus offline validation across the line.

## Support Categories

| Category | Meaning |
| --- | --- |
| `Supported` | Supported for the documented common NiFi `2.x` feature set. |
| `Production-proven` | Proven beyond focused kind-only coverage. No entries are claimed yet in this repository. |
| `Focused-runtime-proven` | Proven in focused runtime workflows in this repository, usually on kind. |
| `Prepared / render-validated` | Configuration and render path exist, but runtime proof is not yet claimed. |
| `Planned` | Documented direction, not yet implemented as a supported runtime path. |

## Common NiFi 2.x Feature Set

The supported NiFi `2.x` line shares this common feature set:

| Feature group | `2.0.x` through `2.8.x` |
| --- | --- |
| `charts/nifi-platform` managed install | Yes |
| safe rollout | Yes |
| hibernation and restore | Yes |
| cert-manager integration | Yes |
| OIDC | Yes |
| LDAP | Yes |
| native API metrics | Yes |
| exporter metrics | Yes |
| autoscaling advisory | Yes |
| autoscaling enforced scale-up | Yes |
| autoscaling enforced scale-down | Yes |
| KEDA integration | Yes |

The table above is intentionally the common set only. Version-specific focused integrations that are currently documented on NiFi `2.8.0`, such as Flow Registry Client workflows, stay documented separately instead of being implied across the whole line.

## Supported NiFi Versions

| NiFi version | Common feature set | Evidence |
| --- | --- | --- |
| `2.0.x` | Supported | Focused runtime anchor on `2.0.0` plus offline validation. |
| `2.1.x` | Supported | Offline validation against the same common feature set. |
| `2.2.x` | Supported | Offline validation against the same common feature set. |
| `2.3.x` | Supported | Offline validation against the same common feature set. |
| `2.4.x` | Supported | Offline validation against the same common feature set. |
| `2.5.x` | Supported | Offline validation against the same common feature set. |
| `2.6.x` | Supported | Offline validation against the same common feature set. |
| `2.7.x` | Supported | Offline validation against the same common feature set. |
| `2.8.x` | Supported | Focused runtime anchor on `2.8.0` plus offline validation. |
| `1.x` | Not supported | Out of scope. |

## Focused Runtime Anchors

Main focused compatibility commands:

- `make kind-nifi-compatibility-fast-e2e`
- `make kind-nifi-compatibility-fast-e2e-reuse`

Shared contract used for both runtime anchors:

- install through `charts/nifi-platform`
- secured cluster startup and health
- basic single-user platform readiness
- native API metrics through the dedicated metrics `Service` and named `ServiceMonitor` resources
- one bounded controller-owned enforced scale-up from `2 -> 3`

The shared harness does not need version-specific values files. It only switches the NiFi image tag inline and keeps controller and chart behavior the same across anchors.

## Install Models

| Install model | Status | Notes |
| --- | --- | --- |
| `charts/nifi-platform` managed install | Focused-runtime-proven | Standard customer path and the primary shared NiFi `2.x` compatibility install surface. |
| `charts/nifi-platform` managed + cert-manager | Focused-runtime-proven | cert-manager must already exist in the cluster. |
| `charts/nifi-platform` standalone mode | Prepared / render-validated | Rendered and supported as a chart shape, but the main product story is managed mode. |
| `charts/nifi` standalone app chart | Focused-runtime-proven | Supported secondary path with dedicated focused proofs outside the shared platform-chart matrix. |

## Feature Compatibility

| Feature | Status | Notes |
| --- | --- | --- |
| Safe rollout | Focused-runtime-proven | Controller-managed, health-gated rollout sequencing. |
| Hibernation and restore | Focused-runtime-proven | Controller-owned lifecycle flow. |
| cert-manager integration | Focused-runtime-proven | cert-manager is a prerequisite. |
| OIDC | Focused-runtime-proven | First-class managed auth option. The richer browser-flow group-claims proof remains conservative on the current local Keycloak `26.x` path. |
| LDAP | Focused-runtime-proven | First-class managed auth option. |
| Native API metrics | Focused-runtime-proven | Primary metrics path and part of the shared NiFi `2.x` compatibility contract. |
| Exporter metrics | Focused-runtime-proven | GA as an optional bounded secondary mode. Native API metrics remain the primary recommendation. |
| Site-to-site metrics | Focused-runtime-proven | GA as an optional bounded sender-side typed mode. Dedicated focused proof exists, but it is intentionally outside the common NiFi `2.x` compatibility line claim on this page. |
| Autoscaling advisory | Focused-runtime-proven | Primary controller-owned recommendation path. |
| Autoscaling enforced scale-up | Focused-runtime-proven | Primary controller-owned execution path and part of the shared NiFi `2.x` compatibility contract as a bounded `2 -> 3` proof. |
| Autoscaling enforced scale-down | Focused-runtime-proven | Production-ready for the bounded one-step, conservative, controller-owned path, with deeper dedicated proof outside the shared version matrix. |
| KEDA integration | Focused-runtime-proven | GA for bounded external scale-up and controller-mediated external downscale intent through `NiFiCluster` `/scale`. The controller remains the sole executor. |
| Linkerd compatibility profile | Focused-runtime-proven | Bounded optional profile for meshed NiFi pods only. The controller remains mesh-agnostic, and the chart applies pod injection plus opaque-port annotations for the supported internal NiFi TCP ports. |
| Istio sidecar compatibility profile | Focused-runtime-proven | Bounded optional sidecar-mode profile for meshed NiFi pods only. The controller remains mesh-agnostic, and the chart applies pod injection plus probe-rewrite and startup annotations for the supported NiFi workload path. |
| Istio Ambient compatibility profile | Focused-runtime-proven | Bounded optional Ambient L4 profile for labeled NiFi pods only. The controller remains mesh-agnostic, and the chart applies pod-template enrollment labels without adding sidecars or waypoint behavior. |
| GitHub Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| GitLab Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| Bitbucket Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| Azure DevOps Flow Registry Client | Prepared / render-validated | Prepared catalog definition only. |

## Exporter Metrics Boundary

GA scope:

- install through `charts/nifi-platform`
- optional `observability.metrics.mode=exporter`
- one chart-owned exporter `Deployment`, `Service`, and `ServiceMonitor`
- secured upstream NiFi reachability to `/nifi-api/flow/metrics/prometheus`
- secured supplemental reachability to `/nifi-api/flow/status`
- Prometheus-scrapable exporter `/metrics`
- relayed flow Prometheus metrics
- selected supplemental gauges derived from `/nifi-api/flow/status`
- mounted auth Secret rotation recovery without exporter pod restart
- trust-manager-backed CA consumption through the existing bounded bundle-consumer path

Outside the GA claim:

- exporter as the default or recommended primary metrics path
- JVM or broader system-diagnostics metric families
- broader trust-manager output-format claims beyond the proven PEM bundle-consumer path
- downstream consumer shapes beyond the documented chart-owned `Service` and `ServiceMonitor`

## Site-to-Site Metrics Boundary

GA scope:

- install through `charts/nifi-platform`
- optional `observability.metrics.mode=siteToSite`
- `observability.metrics.siteToSite.enabled=true`
- one typed sender-side `SiteToSiteMetricsReportingTask`
- one bounded `StandardRestrictedSSLContextService` when secure transport is used
- `auth.type=none` for `http://` receivers
- `auth.type=workloadTLS` or `auth.type=secretRef` for `https://` receivers
- required `auth.authorizedIdentity` for secure receiver authorization
- complete `auth.secretRef.*` material keys when `auth.type=secretRef`
- `RAW` or `HTTP` transport within the typed contract
- `AmbariFormat` only
- focused end-to-end proof for secure sender bootstrap, receiver-side authorized-identity policy checks, peer discovery, and live delivery

Outside the GA claim:

- status export
- provenance export
- generic Reporting Task, Controller Service, or NiFi runtime-object management
- receiver topology, trust of sender certs, receiver-side user and policy lifecycle, long-lived credentials, and reverse-proxy routing assumptions
- receiver automation beyond the proof-only harness
- record-writer ownership beyond the fixed `AmbariFormat` path
- enterprise-auth sender bootstrap beyond the current `auth.mode=singleUser` reconciliation path

## Environment Scope

| Environment | Status | Notes |
| --- | --- | --- |
| kind | Focused-runtime-proven | Current runtime proof baseline. |
| AKS | Prepared / render-validated | Primary target environment, but no real-cluster runtime proof is claimed in this slice. |
| OpenShift | Prepared / render-validated | Secondary target environment, with published readiness guidance and no real-cluster runtime proof claimed in this slice. |

## Bounded Linkerd Profile

Supported profile:

- install through `charts/nifi-platform`
- compose `examples/platform-managed-values.yaml` with `examples/platform-managed-linkerd-values.yaml`
- mesh only the NiFi StatefulSet pods
- keep the controller outside the mesh and unchanged
- mark the NiFi cluster protocol and load-balance ports opaque by default
- keep NiFi HTTPS on `8443` non-opaque in the documented baseline profile
- preserve direct pod-to-pod traffic through the headless Service

Focused proof command:

- `make kind-linkerd-fast-e2e`

What that proof currently covers:

- Linkerd control plane installs successfully on kind
- the NiFi StatefulSet receives `linkerd-proxy`
- secured cluster startup and health still converge
- one direct headless-Service pod-to-pod HTTPS path still works from `nifi-0` to `nifi-1`

What remains intentionally unproven or operator-owned:

- meshing the controller
- namespace-wide automatic injection instead of the workload overlay
- ingress-controller-specific Linkerd behavior
- Linkerd policy resources, viz extension, or per-route telemetry
- exporter metrics and typed Site-to-Site features under Linkerd
- cloud-specific Linkerd behavior on AKS or OpenShift

## Bounded Istio Sidecar Profile

Supported profile:

- install through `charts/nifi-platform`
- compose `examples/platform-managed-values.yaml` with `examples/platform-managed-istio-values.yaml`
- support the bounded Istio sidecar profile separately from the Ambient profile on this page
- enable Istio sidecar injection on the NiFi namespace only; keep the controller namespace outside the mesh
- mesh only the NiFi StatefulSet pods
- keep the controller outside the mesh and unchanged
- apply explicit pod annotations for sidecar injection, probe rewrite, and waiting for the sidecar proxy before NiFi starts
- preserve direct pod-to-pod traffic through the headless Service

Focused proof command:

- `make kind-istio-fast-e2e`

What that proof currently covers:

- Istio control plane installs successfully on kind
- the NiFi StatefulSet receives `istio-proxy`
- secured cluster startup and health still converge under the bounded sidecar profile
- one direct headless-Service pod-to-pod HTTPS path still works from `nifi-0` to `nifi-1`
- the controller remains outside the mesh

What remains intentionally unproven or operator-owned:

- ambient mode, ztunnel, or waypoint-based Istio data planes
- meshing the controller
- product-owned management of namespace injection labels or revision selection
- ingress, Gateway API, or VirtualService-based Istio exposure design
- exporter metrics and typed Site-to-Site features under Istio
- cloud-specific Istio behavior on AKS or OpenShift

## Bounded Istio Ambient Profile

Supported profile:

- install through `charts/nifi-platform`
- compose `examples/platform-managed-values.yaml` with `examples/platform-managed-istio-ambient-values.yaml`
- support Istio Ambient L4 mode only; no waypoint or L7 Ambient profile is included in this slice
- enroll only the NiFi StatefulSet pods through the pod-template label `istio.io/dataplane-mode=ambient`
- keep the controller outside Ambient and unchanged
- preserve the existing Kubernetes probes with no sidecar probe-rewrite path
- preserve direct pod-to-pod traffic through the headless Service

Focused proof command:

- `make kind-istio-ambient-fast-e2e`

What that proof currently covers:

- Istio Ambient control plane installs successfully on kind
- `ztunnel` becomes ready for the bounded runtime profile
- the NiFi StatefulSet pods carry `istio.io/dataplane-mode=ambient`
- the NiFi pods remain sidecar-free under the Ambient profile
- secured cluster startup and health still converge under the bounded Ambient profile
- one direct headless-Service pod-to-pod HTTPS path still works from `nifi-0` to `nifi-1`
- the controller remains outside the mesh

What remains intentionally unproven or operator-owned:

- namespace-wide Ambient enrollment instead of the workload overlay
- waypoint proxies, L7 policy, or service-level Ambient extensions
- meshing the controller
- ingress, Gateway API, or VirtualService-based Istio exposure design
- exporter metrics and typed Site-to-Site features under Ambient
- cloud-specific Ambient behavior on AKS or OpenShift

## Conservative Claims Left Intentionally Conservative

- no production-proven cloud runtime claim is made yet
- no claim is made beyond Apache NiFi `2.0.x` through `2.8.x`
- no claim is made that version-specific integrations such as Flow Registry Client workflows are supported across the whole line unless this page says so explicitly
- richer ingress-backed OIDC browser-flow proof is still conservative on kind
- Linkerd support is bounded to the documented NiFi workload profile; it is not a generic service-mesh support layer
- Istio support is bounded to the documented sidecar-mode and Ambient workload profiles; it is not a generic service-mesh support layer
- smarter drainability selection, richer capacity reasoning, and bulk or multi-node autoscaling policies remain future work
