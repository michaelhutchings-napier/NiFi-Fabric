# Architecture Summary

NiFi-Fabric is built around a small, explainable split of responsibilities.

## Product Components

- `charts/nifi-platform`: standard customer install chart
- `charts/nifi`: reusable NiFi app chart
- `NiFiCluster`: thin operational API for managed mode
- controller: lifecycle and safety coordinator for the managed path

## Ownership Model

### Helm owns

- standard Kubernetes resources
- the NiFi `StatefulSet`
- Services, PVCs, ingress or Route resources
- Secret references
- cert-manager `Certificate` resources when that mode is enabled
- optional trust-manager `Bundle` resources when that mode is enabled
- metrics Services and ServiceMonitors
- prepared Flow Registry Client catalog files
- runtime-managed Parameter Context config and bounded external-parameter references
- runtime-managed versioned-flow import config bundles
- bounded authz bootstrap bundles for mutable flow work

### NiFi owns

- NiFi-native clustering behavior
- NiFi-native auth provider behavior
- NiFi-native API and runtime signals
- persisted file-provider authorization state after chart bootstrap

### The controller owns

- managed rollout sequencing
- TLS restart policy decisions
- hibernation and restore orchestration
- controller-owned autoscaling recommendations and execution
- lifecycle precedence and safety gating
- explicit status and event reporting

## One Lifecycle Control Plane

The controller remains the only executor of destructive lifecycle actions in managed mode.

That includes:

- rollout pod deletion sequencing
- hibernation and restore sequencing
- controller-owned autoscaling execution

This is why direct autoscaler ownership of the NiFi `StatefulSet` is not the product architecture.

## Autoscaling Architecture

Primary model:

- controller-owned autoscaling
- `Disabled`, `Advisory`, and `Enforced` modes
- one-step, conservative scale-down
- enforced scale-down depends on durable low-pressure evidence rather than a single quiet poll
- low-pressure evidence stays intentionally simple and explainable: repeated zero-backlog observations, low executor activity when thread counts are available, extra consecutive-sample requirements when queue evidence is incomplete, stabilization, and cooldown
- disconnect, offload, and post-removal settle work stay restart-safe and resumable inside the same controller-owned execution state
- stalled destructive work prefers explicit blocked or timed-out states with stage-specific diagnostics over risky retry loops or broader remediation behavior

Optional experimental extension:

- KEDA writes external intent to `NiFiCluster`
- the controller still decides whether a safe scale action should happen

## Observability Architecture

Primary metrics path:

- `observability.metrics.mode=nativeApi`
- dedicated chart-owned metrics `Service` plus named `ServiceMonitor` resources
- provider-agnostic machine-auth Secret contract
- focused live runtime proof for secured flow metrics scraping
- recommended production path for customers by default

Experimental or prepared paths:

- `exporter` is a supported secondary metrics path for environments that want a clean `/metrics` endpoint
- the exporter republishes the secured flow Prometheus endpoint and can append selected controller-status gauges from `/nifi-api/flow/status`
- the exporter keeps local liveness separate from upstream-aware readiness and rereads mounted auth material without requiring a pod restart
- `siteToSite` stays optional and is now a typed metrics-export capability instead of a generic NiFi runtime-object framework
- the public API remains bounded to one metrics use case under `observability.metrics.siteToSite`
- the typed contract now includes the receiver-authorized sender identity for secure Site-to-Site modes so the destination-side trust and policy requirement stays explicit and customer-visible
- the app chart owns only the minimum internal NiFi objects required for that use case:
- one `SiteToSiteMetricsReportingTask`
- one `StandardRestrictedSSLContextService` when secure site-to-site transport is enabled
- `siteToSiteStatus` is the next optional typed Site-to-Site capability and remains separate from `observability.metrics.mode`
- the public API stays use-case-specific under `observability.siteToSiteStatus` instead of broadening into generic Reporting Task or Controller Service management
- the typed status contract is intentionally smaller than the metrics contract and is limited to enablement, destination, auth, secure receiver identity, and explicit transport settings plus an optional source instance URL override
- the app chart owns only the minimum internal NiFi objects required for that use case:
- one `SiteToSiteStatusReportingTask`
- one `StandardRestrictedSSLContextService` when secure site-to-site transport is enabled
- `siteToSiteProvenance` is the next optional typed Site-to-Site capability and also remains separate from `observability.metrics.mode` and `observability.siteToSiteStatus`
- the public API stays use-case-specific under `observability.siteToSiteProvenance` instead of broadening into generic Reporting Task or Controller Service management
- the typed provenance contract is intentionally small and limited to enablement, destination, auth, secure receiver identity, explicit transport settings, an optional source instance URL override, and a bounded initial cursor setting for first-run provenance export
- the app chart owns only the minimum internal NiFi objects required for that use case:
- one `SiteToSiteProvenanceReportingTask`
- one `StandardRestrictedSSLContextService` when secure site-to-site transport is enabled
- `parameterContexts` is a separate optional typed runtime-managed config feature for bounded Parameter Context definitions
- the public API stays use-case-specific under `parameterContexts` and does not add arbitrary graph-editing, generic runtime-object, or generic Controller Service management
- the typed contract is intentionally narrow and limited to context names, descriptions, non-sensitive inline parameters, sensitive Kubernetes Secret references, small external-parameter-provider references that document operator-managed NiFi providers without creating them, and optional attachment declarations for direct root-child process groups only
- the app chart owns only the declared Parameter Contexts it creates, updates, or deletes in NiFi, the rendered catalog and runtime reconcile files, the bounded root-child attachment mutations it performs, and the Kubernetes Secret references used for sensitive values
- the chart performs live reconcile for those owned contexts instead of depending on restart-only bootstrap, supports the current single-user path plus enterprise auth modes when an explicit bootstrap admin identity is available for the local trusted-proxy path, deletes removed owned contexts safely, and can attach owned contexts only within the declared direct-root-child process-group scope
- `providerRefs` remain bounded and honest: they stay reference-only in this slice and do not create or refresh NiFi Parameter Providers
- `versionedFlowImports` is the next optional typed runtime-managed config feature for bounded flow import and version selection
- the public API stays use-case-specific under `versionedFlowImports` and is intentionally limited to one selected registry client reference, bucket, flow name, one selected version identifier or `latest`, one intended root-child import target name, and optional direct Parameter Context references
- the app chart owns only the declared root-child imported process-group instances it creates from that config, plus the rendered import bundle and status files used for restart-scoped reconciliation
- the chart resolves a live registry client, imports the selected versioned flow into the named root child process group, and can attach one declared Parameter Context reference when present
- in the current bounded slice, runtime-managed import also requires a prepared GitHub Flow Registry Client definition so the bootstrap can fetch the selected saved-flow snapshot when NiFi only returns version metadata; this does not make the product a generic registry-client or flow-runtime manager
- the chart does not become a generic flow-runtime API, does not perform arbitrary process-group mutation, does not add controller-managed ongoing synchronization, and does not add flow CRDs
- no generic Reporting Task, Controller Service, or NiFi runtime-object public API is introduced
- record-writer ownership, proxy-controller-service ownership, and any broader runtime-object lifecycle APIs remain future work
- destination receiver topology, the receiver-side `/site-to-site` and `/controller` read grants, the destination input-port write grant for that identity, long-lived credential lifecycle, any reverse-proxy routing assumptions, NiFi-side Parameter Provider creation, live Flow Registry Client lifecycle beyond the selected reference, broader process-group Parameter Context assignment, and any arbitrary NiFi-side flow edits remain explicit operator-owned concerns
- current runtime ownership is intentionally chart-scoped and bootstrap-scoped rather than controller-owned orchestration

Current conservative boundary:

- `nativeApi` runtime proof is still centered on the secured flow Prometheus endpoint
- exporter runtime proof adds one second secured endpoint, `/nifi-api/flow/status`, through the chart-owned exporter path
- site-to-site runtime proof is intentionally bounded to typed reporting-task and SSL-context bootstrap plus a proof-only receiver harness; full receiver-pipeline ownership remains narrower than the generic site-to-site problem space
- parameter context support is intentionally bounded to declared create, update, delete, and direct-root-child attachment reconciliation; it still does not claim arbitrary graph editing or Parameter Provider creation
- versioned flow import support is intentionally bounded to restart-scoped import and selected-version enforcement for declared root-child process groups backed by the prepared GitHub client path; broader drift enforcement, arbitrary process-group edits, and ongoing synchronization remain out of scope
- JVM or system-diagnostics metrics are not yet runtime-proven
- machine-auth Secret bootstrap is partially automated, but machine principal provisioning and IdP write-back remain out of scope

## Trust Distribution Architecture

Primary TLS path:

- external Secret or cert-manager-issued NiFi TLS material
- chart-owned mounting and restart-trigger wiring
- controller-owned TLS drift observation and safe restart policy

Optional trust-manager extension:

- `charts/nifi-platform` can render a trust-manager `Bundle` for shared CA distribution
- the Bundle targets the NiFi release namespace only
- `charts/nifi` can consume that bundle for:
- secured metrics CA trust
- optional extra CA import into NiFi's runtime truststore for outbound trust
- the controller does not orchestrate trust bundles
- supported Bundle targets stay bounded:
- ConfigMap targets for PEM distribution
- Secret targets when the upstream trust-manager installation allows secret targets
- optional additional PKCS12 and JKS outputs when explicitly configured

Current conservative boundary:

- trust-manager is optional and disabled by default
- cert-manager remains the primary supported certificate lifecycle
- trust-manager support stays focused on CA and trust bundle distribution, not full TLS orchestration
- optional platform-owned TLS CA mirroring can copy the workload `ca.crt` into trust-manager's source namespace
- that mirroring remains chart-owned helper automation, not controller-owned trust orchestration
- trust-manager source Secrets or ConfigMaps can still be operator-provided directly in trust-manager's configured trust namespace

## Install Architecture

Standard customer path:

- one Helm release with `charts/nifi-platform`

Secondary paths:

- standalone `charts/nifi`
- advanced manual assembly for platform teams
- generated manifest bundle rendered from `charts/nifi-platform`

The secondary manifest bundle stays generated from the Helm chart at render time, so Helm remains the source of truth and kustomize-specific chart duplication is avoided.

## Backup and DR Architecture

NiFi-Fabric treats backup, restore, and disaster recovery as a two-layer production concern.

The layers are intentionally separated:

- control-plane backup preserves the declarative platform intent
- data-plane recovery preserves or reconstructs NiFi runtime state and persisted repositories

This separation is important because the platform is intentionally thin:

- Helm and `NiFiCluster` describe how the cluster should be wired and operated
- NiFi and the storage platform determine how queued data, provenance, repositories, and local runtime state survive or recover
- the product does not claim generic NiFi internal object-management or storage orchestration ownership

### Control-Plane Backup

Control-plane backup is the part NiFi-Fabric expects teams to treat as first-class and routine.

The control-plane backup scope is the declarative source of truth for:

- `charts/nifi-platform` values, overlays, and rendered intent
- `charts/nifi` values, overlays, and rendered intent when the standalone path is used
- the `NiFiCluster` resource in managed mode
- chart-managed config such as auth mode, TLS mode, metrics mode, typed Site-to-Site features, Flow Registry Client catalog content, ingress or Route settings, and bounded authz bootstrap bundles
- references to Secrets, ConfigMaps, cert-manager issuers, trust-manager bundles, storage classes, and other cluster prerequisites
- GitOps metadata and release structure used to reapply the platform consistently

Control-plane backup does not by itself preserve:

- queue contents
- FlowFile repository state
- content repository state
- provenance repository state
- NiFi internal runtime state that only exists on local disk
- external Secret manager contents, cert-manager private keys, or trust-manager source objects unless the operator also backs those systems up

Production posture:

- the declarative release inputs should live in Git or an equivalent auditable system of record
- Secret values and issuer material should have their own operator-owned recovery source of truth
- control-plane restore should be possible by recreating prerequisites and reapplying the product-facing Helm release

### Data-Plane Recovery Posture

Data-plane recovery is primarily a storage and NiFi-runtime concern.

What redeploy plus control-plane config can restore:

- the Kubernetes resources rendered by the charts
- the `NiFiCluster` desired lifecycle and autoscaling policy
- TLS, auth, metrics, trust-manager, and Flow Registry Client wiring
- chart-managed NiFi bootstrap configuration
- typed sender-side Site-to-Site runtime objects recreated by the existing bounded bootstrap logic

What requires persistent storage or storage snapshots:

- queued FlowFiles
- FlowFile repository state
- content repository data
- provenance repository state
- any local file-provider state or NiFi runtime state stored on the repository PVCs

The current repository posture is PVC-backed, not stateless. Production DR therefore depends on a storage-class and snapshot design that matches the workload's RPO and RTO needs.

Recommended data-plane posture:

- use durable PVC-backed storage for all four NiFi repositories
- understand whether the chosen platform supports crash-consistent or application-consistent volume snapshots
- define an operator-owned PVC snapshot schedule and retention policy
- test restore of the full repository set together, not one PVC at a time in isolation
- treat restore to mismatched repository generations as a data-loss or corruption risk unless proven safe in the environment

Realistic RPO and RTO guidance:

- control-plane RPO can be close to zero when values, overlays, and `NiFiCluster` intent are managed in Git
- data-plane RPO depends on snapshot cadence and external-system replay capability, not on the platform chart alone
- data-plane RTO depends on storage restore speed, PVC reattachment, NiFi repository recovery time, cluster size, and any post-restore operator checks
- redeploy-only recovery can be fast but is a config recovery, not a queue or repository recovery
- full workload recovery with PVC snapshots should be budgeted as a storage-led recovery exercise, not a simple Helm reinstall

### DR Ownership Boundary

What the product supports:

- a stable declarative install surface through `charts/nifi-platform`, `charts/nifi`, and `NiFiCluster`
- chart-managed wiring for TLS, auth, metrics, trust-manager consumption, Flow Registry Client definitions, and typed Site-to-Site sender features
- controller-owned lifecycle behavior for rollout, TLS restart policy, hibernation, restore, and supported autoscaling execution
- documentation for separating declarative restore from repository recovery

What remains operator- or environment-owned:

- Kubernetes and cloud backup tooling
- PVC snapshot and restore orchestration
- external Secret manager backup and restore
- cert-manager issuer lifecycle, CA hierarchy, and any private-key recovery plan
- trust-manager source object lifecycle and bundle-source recovery
- NiFi data replay strategy when no PVC snapshot is available
- disaster-recovery runbooks, drills, and recovery-time objectives for the target environment
