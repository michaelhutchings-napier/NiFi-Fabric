# NiFi-Fabric

NiFi-Fabric is a Kubernetes platform layer for Apache NiFi 2.x.

It provides a product-facing one-release install path through `charts/nifi-platform`, a reusable standalone app chart in `charts/nifi`, and a thin controller for lifecycle and safety decisions that Helm cannot perform safely on its own.

## Why NiFi-Fabric

- one Helm release is the standard customer install path
- the controller stays focused on safe rollout, TLS restart policy, hibernation, restore, and controller-owned autoscaling
- NiFi-native behavior stays in NiFi, standard Kubernetes resources stay in Helm
- OIDC and LDAP are first-class managed authentication options
- bounded mutable-flow authorization bootstrap is available for chart-managed process-group editing and versioning work
- named viewer, editor, flow-version-manager, and admin bundles are available as the recommended customer-facing authz path
- cert-manager is supported when it already exists in the cluster
- optional trust-manager integration is available for shared CA bundle distribution
- Git-based Flow Registry Clients are the supported modern direction
- observability and metrics are a first-class subsystem instead of an afterthought
- starter dashboards, alert rules, and runbooks are included for production-oriented operations handoff

## Standard Install Path

The standard customer path is `charts/nifi-platform`.

Prerequisites:

- a reachable controller image for the target cluster
- `Secret/nifi-tls`
- `Secret/nifi-auth`
- cert-manager only when you choose cert-manager TLS mode
- trust-manager only when you choose the optional trust-manager bundle overlay

Managed platform install:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

Managed platform install with cert-manager:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-cert-manager-values.yaml
```

Managed platform install with optional trust-manager bundle distribution:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-trust-manager-values.yaml
```

Focused trust-manager proof:

```bash
make kind-platform-managed-trust-manager-fast-e2e
```

Focused trust-manager-backed native metrics proof:

```bash
make kind-metrics-native-api-trust-manager-fast-e2e
```

Secondary manifest-bundle path:

```bash
make render-platform-managed-bundle
kubectl apply -f dist/nifi-platform-managed-bundle.yaml
```

## Documentation

Start here:

- [Docs Home](docs/README.md)
- [Start Here](docs/start-here.md)
- [Features](docs/features.md)
- [Compatibility](docs/compatibility.md)

Install:

- [Install with Helm](docs/install/helm.md)
- [Advanced Install Paths](docs/install/advanced.md)

Manage NiFi:

- [TLS and cert-manager](docs/manage/tls-and-cert-manager.md)
- [Authentication](docs/manage/authentication.md)
- [Autoscaling](docs/manage/autoscaling.md)
- [Parameter Contexts](docs/manage/parameters.md)
- [Flows](docs/manage/flows.md)
- [Flow Registry Clients](docs/manage/flow-registry-clients.md)
- [Hibernation and Restore](docs/manage/hibernation-and-restore.md)
- [Observability and Metrics](docs/manage/observability-metrics.md)
- [Backup, Restore, and Disaster Recovery](docs/dr.md)

Reference and support:

- [Architecture Summary](docs/architecture.md)
- [Verification and Support Levels](docs/testing.md)
- [NiFiCluster Reference](docs/reference/nificluster.md)
- [Platform Chart Values Reference](docs/reference/nifi-platform-values.md)
- [App Chart Values Reference](docs/reference/app-chart-values.md)
- [Operations and Troubleshooting](docs/operations.md)
- [Experimental Features](docs/experimental-features.md)
- [Roadmap](docs/roadmap.md)

## Compatibility Summary

NiFi-Fabric targets Apache NiFi `2.0.x` through `2.8.x`.

- the supported NiFi `2.x` line is `2.0.x` through `2.8.x`
- the repo keeps one bounded focused runtime compatibility contract anchored at `apache/nifi:2.0.0` and `apache/nifi:2.8.0`
- that shared contract uses `charts/nifi-platform` plus secured health, basic auth readiness, native API metrics, and one bounded controller-owned scale-up from `2 -> 3`
- the broader `2.0.x` through `2.8.x` support claim follows the same common feature set and your offline validation, not a forked per-version test flow
- NiFi `1.x` is not supported
- AKS is a primary target, but current repo proof is still kind-first
- OpenShift is supported as a prepared secondary target and remains conservative until real-cluster proof is recorded
- richer ingress-backed OIDC browser-flow proof is still conservative on the current local Keycloak `26.x` path

See [Compatibility](docs/compatibility.md) for the detailed matrix.

## Product Position

- `charts/nifi-platform` is the standard product install
- `charts/nifi` is the standalone-capable app chart
- built-in controller-owned autoscaling is the primary autoscaling model
- advisory autoscaling is production-ready as the bounded controller-owned recommendation path
- enforced scale-up is production-ready as the bounded controller-owned execution path
- enforced scale-down is production-ready for the bounded controller-owned sequential one-node path, including bounded sequential multi-step episodes
- KEDA external scale-up intent is GA as an optional, secondary integration on top of the built-in controller-owned autoscaler
- when KEDA is enabled, it targets `NiFiCluster` `/scale` as a runtime-managed intent path; users should not hand-author `spec.autoscaling.external.requestedReplicas` in Helm values or expect KEDA to own the NiFi `StatefulSet`
- KEDA external intent now reports the raw request, the controller-bounded intent, and the current handling state through `status.autoscaling.external.*`, including actionable, deferred, blocked, or ignored states
- focused repo tests now prove that rollout, TLS, restore, hibernation, degraded state, and already-running destructive work can block KEDA intent cleanly without letting it bypass controller-owned lifecycle safety
- KEDA intent now has an explicit restart-safe support story: the runtime-managed `/scale` request survives controller restart, stays visible in status, and converges only after the higher-precedence conflict clears
- controller-mediated KEDA external downscale is now GA through the same bounded safe scale-down path: the request may be accepted, blocked, deferred, ignored, or resumed later, but it never bypasses low-pressure qualification, lifecycle precedence, or one-step-at-a-time node removal
- the starter operations package now includes KEDA-specific runbooks and alert guidance for received, ignored, blocked, deferred, and GitOps-conflicted external intent
- controller events now distinguish `AutoscalingExternalIntentBlocked`, `AutoscalingExternalIntentDeferred`, and `AutoscalingExternalIntentIgnored` so operators can tell lifecycle precedence apart from cooldown or low-pressure waiting
- scale-up recommendations now stay bounded and explainable: root-process-group backlog, queued bytes, timer-driven thread saturation, and CPU saturation are still the only current inputs, but single-signal pressure now needs corroboration or consecutive evaluations before it becomes a stronger recommendation
- autoscaling recommendation messages now also explain the expected bounded capacity effect of the next step, such as adding executor headroom for backlog pressure, adding CPU headroom for sustained saturation, or removing one node only when the current quiet envelope remains convincing
- the controller now distinguishes bounded capacity evidence tiers such as pressure building, capacity tight, and capacity clearly insufficient using the same small signal set: backlog pressure, queued bytes, timer-driven thread saturation, CPU saturation, and persistence across evaluations
- enforced scale-down stays one-node-at-a-time and now requires durable low-pressure evidence before the controller removes any node
- smarter scale-down candidate selection is now part of the supported bounded model: the controller qualifies the actual StatefulSet `N -> N-1` removal pod from live pod state and blocks with explicit candidate reasons when that pod is missing, terminating, or not Ready
- lower ordinals are now rejected explicitly rather than silently treated as fallback candidates, because one-step StatefulSet scale-down still cannot safely widen deletion to a different pod
- bounded multi-node scale-down is now supported as a sequential controller-owned episode: each additional removal still runs as its own disconnect, offload, delete, and settle step with fresh requalification and no concurrent destructive work
- `spec.autoscaling.scaleDown.maxSequentialSteps` caps how many one-node removals the controller may complete in a single scale-down episode, and execution status now reports planned and completed episode steps while the episode is active
- when scale-down disconnect, offload, or post-removal settle work stalls, the controller now keeps the step blocked and restart-safe with stage-specific diagnostics instead of silently retrying risky destructive work
- autoscaling diagnostics now make the requested, recommended, and executing states explicit through `status.autoscaling.external.*`, `status.autoscaling.recommendedReplicas`, `status.autoscaling.execution.*`, and context-rich `lastScalingDecision`
- mutable-flow authorization bootstrap stays chart-first and controller-free
- GitHub, GitLab, and Bitbucket Flow Registry Client paths are runtime-proven on NiFi `2.8.0`
- a user-driven GitHub versioned-flow save-to-registry workflow is focused-runtime-proven on NiFi `2.8.0`
- Azure DevOps Flow Registry Client remains prepared and render-validated
- Parameter Context support is available as an optional typed runtime-managed feature for bounded Parameter Context creation, live update, deletion, and direct root-child attachment, not as generic flow-runtime management
- bounded versioned flow import and version selection are available as an optional typed runtime-managed feature for declared root-child import targets, including live version reconcile, explicit ownership markers, and selected-version attachment without provider write-back, not as generic flow-runtime management
- a bounded restore workflow is now focused-runtime-proven on the platform chart path for control-plane reinstall plus registry-client reconnect, runtime-managed Parameter Context recovery, and selected-flow import from registry-backed content
- native API metrics are the primary, recommended metrics path and are runtime-proven on kind
- exporter metrics are an optional experimental secondary path and are runtime-proven on kind
- site-to-site metrics, status, and provenance export are optional typed runtime paths for bounded sender-side use cases, not a generic NiFi runtime-object framework
- optional trust-manager integration distributes shared CA bundles without moving TLS orchestration into the controller
- backup and DR are documented as a first-class production posture with explicit separation between declarative platform recovery and PVC-backed NiFi data recovery
- a thin control-plane backup or recovery MVP now exports Helm values, rendered manifest intent, sanitized `NiFiCluster` intent, and reference inventories without adding a second product control plane
- the repo now includes a starter operations package for dashboards, alerting, and runbooks; teams still need to adapt it to their Prometheus, Grafana, and incident-routing conventions

## Experimental Features

These features are available but intentionally marked experimental:

- site-to-site metrics export
- site-to-site status export
- site-to-site provenance export

## Metrics Runtime Proof

The repo now carries a focused metrics runtime proof matrix:

- `make kind-metrics-fast-e2e`
- `make kind-metrics-fast-e2e-reuse`

That matrix proves:

- secured `nativeApi` scraping with a dedicated chart-managed metrics `Service` and named `ServiceMonitor` resources
- experimental `exporter` mode with its companion `Deployment`, `Service`, and `ServiceMonitor`
- the documented machine-auth Secret and CA Secret contract used by both modes

Focused typed Site-to-Site proof is also available through:

- `make kind-metrics-site-to-site-fast-e2e`
- `make kind-metrics-site-to-site-fast-e2e-reuse`
- `make kind-site-to-site-status-fast-e2e`
- `make kind-site-to-site-status-fast-e2e-reuse`
- `make kind-site-to-site-provenance-fast-e2e`
- `make kind-site-to-site-provenance-fast-e2e-reuse`

Current conservative boundary:

- `nativeApi` is runtime-proven for the secured `/nifi-api/flow/metrics/prometheus` endpoint
- `nativeApi` is also runtime-proven consuming a trust-manager-distributed CA bundle through the optional platform trust-manager overlay
- `nativeApi` is the recommended production path unless you have a clear reason to prefer the exporter shape
- `exporter` is runtime-proven for direct secured reachability to `/nifi-api/flow/metrics/prometheus` and `/nifi-api/flow/status` from the chart-owned exporter pod
- `exporter` exposes a Prometheus-scrapable `/metrics` endpoint that relays live NiFi metric families from the secured flow source
- `exporter` is runtime-proven for selected controller-status gauges derived from `/nifi-api/flow/status`
- `exporter` is runtime-proven for upstream-aware readiness and mounted auth Secret rotation without restarting the exporter pod
- `siteToSite` is now runtime-proven end to end as a typed metrics-export path that creates exactly one `SiteToSiteMetricsReportingTask` and one `StandardRestrictedSSLContextService` when secure transport is enabled
- `siteToSite` proof now covers typed sender bootstrap, explicit receiver-authorized identity wiring, secure receiver peer discovery, receiver-side policy binding checks, and live delivery to a real Site-to-Site receiver on kind through the product-facing chart path
- `siteToSite` remains bounded to `AmbariFormat`, an explicit secure receiver auth contract, a proof-only receiver harness, and the current single-user bootstrap path for local NiFi API management
- `siteToSiteStatus` is now a second typed Site-to-Site path that creates exactly one `SiteToSiteStatusReportingTask` and one `StandardRestrictedSSLContextService` when secure transport is enabled
- `siteToSiteStatus` keeps JSON status payload shape, platform, filters, and batching fixed behind the typed API so we do not add generic Reporting Task or Controller Service ownership
- `siteToSiteProvenance` is now a third typed Site-to-Site path that creates exactly one `SiteToSiteProvenanceReportingTask` and one `StandardRestrictedSSLContextService` when secure transport is enabled
- `siteToSiteProvenance` keeps provenance cursor and batching intentionally small in the public API so we do not turn this into generic reporting-task management
- `siteToSite` is not a generic Reporting Task, Controller Service, or NiFi runtime-object framework
- two named native scrape profiles are proven, but they still scrape the same flow Prometheus endpoint at different cadence
- JVM or system-diagnostics metrics are not yet runtime-proven
- full destination receiver topology, long-lived destination-side user or policy lifecycle management, proxy-controller-service wiring, non-Ambari record-writer ownership, broader Site-to-Site status filtering or formatting controls, and broader provenance event-selection or batching controls remain future work

Operators still provide, out of band:

- a machine credential already accepted by NiFi, or a pre-minted token
- the machine principal lifecycle itself, including IdP-side provisioning and rotation policy
- any trust-manager trust namespace and Secret-target permissions required by your chosen trust-manager installation

The focused kind proof can mint a short-lived NiFi access token for the metrics Secret. Production deployments still need an operator-managed credential or rotation path that stays valid for steady-state scraping.

## NiFi 2.x Compatibility Matrix

The repo now carries one shared focused NiFi `2.x` compatibility contract:

- `make kind-nifi-compatibility-fast-e2e`
- `make kind-nifi-compatibility-fast-e2e-reuse`

That contract is anchored at `apache/nifi:2.0.0` and `apache/nifi:2.8.0`, and the broader supported line remains `2.0.x` through `2.8.x`:

- install through `charts/nifi-platform`
- secured cluster startup and basic single-user readiness
- native API metrics through the dedicated metrics `Service` and named `ServiceMonitor` resources
- one bounded controller-owned enforced scale-up from `2 -> 3`

Current boundary:

- the compatibility contract is intentionally bounded to the common baseline contract above
- site-to-site sender paths are not part of this matrix
- deeper focused auth, cert-manager, Flow Registry Client, restore, and broader autoscaling proofs remain on their dedicated targeted gates

## KEDA Confidence Boundary

The repo now carries two layers of KEDA confidence:

- focused kind runtime proof for the bounded external scale-up path and the opt-in best-effort external downscale path
- focused repo tests for conflict and supportability behavior such as rollout, TLS, restore, hibernation, degraded-state, destructive-work, and controller-restart interactions

Current boundary:

- GA: external scale-up intent written by KEDA through `NiFiCluster` `/scale`, with controller-owned bounded execution and lifecycle precedence
- GA: controller-mediated external downscale intent through the same bounded safe scale-down path
- KEDA still does not execute scale actions directly
- scale-up may converge after a conflict clears, but only through the normal controller-owned one-step path
- external downscale still does not guarantee pod removal after a conflict clears; the existing safe scale-down checks must re-qualify before any node is removed

## Install Surface Note

The supported install surfaces are:

- `charts/nifi-platform` for the standard one-release platform path
- a generated manifest bundle rendered from `charts/nifi-platform` for advanced manifest-based workflows
- `charts/nifi` for standalone or advanced assembly

Helm remains the primary recommendation because it stays the source of truth for the product install surface. The generated bundle is a secondary path for teams that prefer applying rendered manifests.

## Conservative Claims

NiFi-Fabric documentation is intentionally conservative in a few areas:

- AKS and OpenShift guidance is published, but real-cluster runtime proof is not yet claimed here
- KEDA examples and validation now intentionally keep `spec.autoscaling.external.requestedReplicas` runtime-managed at `0` in declarative values so KEDA and GitOps do not appear to be competing autoscalers
- KEDA support remains intentionally narrow: external intent through `NiFiCluster` `/scale`, controller-owned execution only, and no direct `StatefulSet` ownership
- autoscaling scale-down remains intentionally one-node-at-a-time, bounded to the controller-owned model, and limited to the actual StatefulSet removal pod for each step even when multiple sequential removals are planned
- enforced scale-down now waits for repeated zero-backlog observations, low executor activity when thread counts are available, and stabilization or cooldown windows before a removal step is allowed
- in-progress autoscaling scale-down now remains restart-safe across blocked prepare or settle work, re-establishes preparation safely after pod churn, and pauses cleanly when higher-precedence rollout, TLS, hibernation, or restore work takes over
- broader per-node drainability ranking and broader bulk policy depth beyond the current bounded sequential-episode model remain future work until the project has bounded trustworthy evidence that would justify anything beyond the current actual-removal-candidate qualification model
- site-to-site metrics export remains optional, experimental, and intentionally bounded to the typed metrics-export path
- site-to-site status export remains optional, experimental, and intentionally bounded to the typed status-export path
- site-to-site provenance export remains optional, experimental, and intentionally bounded to the typed provenance-export path
- parameter contexts are runtime-managed only within the declared bounded scope of owned context create/update/delete and direct root-child attachment; Parameter Provider creation and generic flow-runtime management remain out of scope
- exporter support remains experimental and intentionally bounded to flow metrics plus selected `/flow/status` gauges
- the user-driven GitHub save-to-registry workflow is separately proven, while bounded runtime-managed flow import is proven only within the declared `versionedFlowImports.*` scope; generic deployment and ongoing synchronization remain out of scope
- trust-manager currently distributes shared CA bundles only; it does not replace cert-manager or move trust orchestration into the controller
- automatic mirroring of the workload TLS `ca.crt` into a trust-manager source Secret is available as an optional chart-owned helper path
- ConfigMap and Secret bundle targets are supported, but current automatic app consumption still centers on the PEM `ca.crt` bundle key
- DR guidance is production-oriented but intentionally does not claim storage snapshot orchestration, provider write-back, or full NiFi internal recovery ownership
- versioned flow import is runtime-managed only within the declared bounded scope; live registry client lifecycle, provider write-back, broader process-group mutation, and ongoing synchronization remain out of scope
- the bounded restore workflow proof is config-and-flow recovery only; it does not claim queue, provenance, content, or other PVC-backed NiFi state replay
