# Verification and Support Levels

This page explains how NiFi-Fabric support claims are grounded in repository proof.

## Verification Layers

NiFi-Fabric uses several layers of verification:

- unit and envtest coverage for controller behavior
- Helm render and lint coverage for chart behavior
- focused kind runtime workflows for supported feature paths

## What Is Proven Today

Focused runtime proof in this repository includes:

- standard managed platform install on kind
- cert-manager integration on kind
- OIDC and LDAP focused auth paths on kind
- native API metrics on kind
- exporter metrics on kind
- optional trust-manager shared CA distribution on kind
- controller-owned autoscaling focused flows on NiFi `2.8.0`
- optional experimental KEDA intent-source flows on NiFi `2.8.0`
- GitHub, GitLab, and Bitbucket Flow Registry Client focused flows on NiFi `2.8.0`
- a GitHub versioned-flow save-to-registry workflow on NiFi `2.8.0`
- a bounded GitHub versioned-flow selection workflow on NiFi `2.8.0`
- a bounded platform restore workflow on NiFi `2.8.0`

Current auth and exposure hardening notes:

- the richer OIDC group-claims overlay is render-validated and now boot-validates cleanly instead of crashing NiFi during `authorizations.xml` load
- the local Keycloak `26.x` browser-flow evaluator for observer, operator, and admin policy enforcement is still under active hardening on kind
- ingress-backed OIDC exposure on kind is still conservative until that evaluator path is green
- AKS and OpenShift auth and exposure guidance remain render-only in this slice

## Platform Chart Runtime Confidence

The platform chart path is part of the ongoing confidence suite through:

- `make kind-platform-managed-fast-e2e`
- `make kind-platform-managed-cert-manager-fast-e2e`
- `make kind-platform-managed-trust-manager-fast-e2e`

What these focused gates prove:

- `charts/nifi-platform` installs the CRD, controller, app chart, and managed `NiFiCluster` in one Helm release
- no manual `kubectl apply` step is required for the standard platform-chart path
- the chart-installed `NiFiCluster` becomes healthy
- the controller observes and manages the chart-installed `NiFiCluster`
- the cert-manager platform overlay works through the same one-release chart path when cert-manager is present
- the optional trust-manager overlay reconciles a shared CA bundle into the NiFi namespace and keeps the managed cluster healthy
- the focused trust-manager proof bootstraps cert-manager first because the current upstream trust-manager chart uses cert-manager resources for its webhook certificate path

## What Is Render-Validated or Prepared

- AKS guidance is published but still conservative
- OpenShift guidance is published but still conservative
- Azure DevOps Flow Registry Client definitions are prepared and render-validated
- versioned-flow import render validation still covers the bounded runtime bundle and target selection contract
- Parameter Context render validation still covers the bounded catalog and external-provider references

## Flow Registry Workflow Proof

The current focused end-to-end workflow command is:

- `make kind-flow-registry-github-workflow-fast-e2e`
- `make kind-flow-registry-github-workflow-fast-e2e-reuse`
- `make kind-versioned-flow-selection-fast-e2e`
- `make kind-versioned-flow-selection-fast-e2e-reuse`
- `make kind-platform-managed-restore-fast-e2e`
- `make kind-platform-managed-restore-fast-e2e-reuse`

What it proves:

- NiFi `2.8.0` starts healthy with the chart-prepared GitHub Flow Registry Client overlay
- the external client is created successfully through the NiFi runtime API
- bucket discovery succeeds against the GitHub-compatible evaluator
- the chart-seeded mutable-flow and flow-version-manager authz bundle is sufficient for child process-group creation and version-control attachment
- a real user-driven save-to-registry workflow succeeds through the NiFi API
- the versioned flow snapshot is written into the external Git-backed registry path and the process group reports attached version-control state

What it does not prove:

- controller-managed flow deployment
- automatic synchronization
- classic NiFi Registry integration
- provider parity beyond the current GitHub workflow proof

## Versioned Flow Selection Proof

The current focused bounded-selection workflow command is:

- `make kind-versioned-flow-selection-fast-e2e`
- `make kind-versioned-flow-selection-fast-e2e-reuse`

What it proves:

- NiFi `2.8.0` starts healthy with the chart-prepared GitHub Flow Registry Client and versioned-flow import overlays plus the runtime-managed Parameter Context overlay
- the external client is created successfully through the NiFi runtime API from the prepared client catalog
- a real versioned flow with the catalog-selected bucket and flow name is written to the GitHub-compatible evaluator through the existing user-driven save workflow
- the chart-rendered bounded selection catalog is present in-cluster with the expected selected registry client, bucket, flow, version, intended target name, and Parameter Context references
- the selected flow and available versions are discoverable live through the NiFi API for that registry client
- `version=latest` resolves to the live latest provider-native version identifier on the focused GitHub workflow path

What it does not prove:

- product-owned automated import into a NiFi process group
- product-owned process-group creation or placement
- product-owned Parameter Context attachment to imported process groups
- controller-managed or continuous version synchronization

## Versioned Flow Import Runtime Proof

The current focused bounded versioned-flow import runtime commands are:

- `make kind-platform-managed-versioned-flow-import-fast-e2e`
- `make kind-platform-managed-versioned-flow-import-fast-e2e-reuse`

What they prove:

- `charts/nifi-platform` installs the bounded `versionedFlowImports.*` config through the standard product path
- the selected live Flow Registry Client is created through the existing GitHub workflow helper path as an operator-owned prerequisite and reused by the import bootstrap
- the focused proof uses a single-node managed platform install, upgrades the release with `versionedFlowImports.*` enabled, hydrates the chart-rendered bounded import bundle into pod `-0`, and executes the bounded bootstrap directly against the running NiFi node
- pod `-0` imports the selected registry-backed flow into the declared root child process group and then attaches or updates the selected version through the NiFi versions API without provider write-back
- the resulting process group exists in NiFi with version-control state for the selected registry, bucket, flow, and resolved version
- the imported process group includes the seeded bounded flow contents from the registry-backed snapshot
- the declared direct Parameter Context attachment is present on the imported process group
- the feature remains bounded to one declared root-child import target per entry and restart-scoped reconciliation

What they do not prove:

- automatic creation of Flow Registry Clients by the product
- automatic preservation of an operator-owned live Flow Registry Client across single-node pod replacement
- arbitrary mutation inside the imported process group
- automatic upgrade to newer registry versions on an ongoing basis
- provider write-back or registry-side commits from the product
- deletion of removed imports

## Parameter Context Runtime Proof

The current focused bounded Parameter Context runtime commands are:

- `make kind-parameter-contexts-runtime-fast-e2e`
- `make kind-parameter-contexts-runtime-fast-e2e-reuse`

What they prove:

- `charts/nifi-platform` installs the bounded `parameterContexts.*` config through the standard product path
- the focused kind proof overlay also enables the bounded mutable-flow bootstrap permission needed to seed one proof-only direct root-child attachment target
- pod `-0` creates the declared Parameter Context in NiFi
- inline non-sensitive values are applied as declared
- sensitive values are loaded from the referenced Kubernetes Secret and applied as sensitive parameters
- the runtime status file records successful live reconciliation and Secret resolution
- a changed declared value is reconciled without replacing the NiFi pod
- the live update path tolerates the normal Kubernetes projected `ConfigMap` and Secret refresh delay without requiring a NiFi pod replacement
- a removed product-owned context is deleted from NiFi
- the declared direct root-child Parameter Context attachment is applied and can be reassigned within the bounded supported scope
- the feature remains bounded to product-owned create/update/delete plus direct root-child attachment and does not broaden into arbitrary graph mutation or generic Parameter Provider management

What they do not prove:

- Parameter Provider creation or refresh
- arbitrary process-group assignment outside the declared direct root-child scope
- enterprise-auth runtime proof; `oidc` and `ldap` are render-validated and supported with explicit `authz.bootstrap.initialAdminIdentity`, but the focused kind runtime proof remains on the standard `singleUser` path

## Autoscaling Runtime Proof

The focused autoscaling scale-down runtime commands are:

- `make kind-autoscaling-scale-down-fast-e2e`
- `make kind-autoscaling-scale-down-fast-e2e-reuse`

What they prove:

- the controller remains the only executor of actual scale-down
- scale-down stays one-step-at-a-time
- a removal step is gated on sustained low-pressure evidence instead of a single quiet sample
- low-pressure needs repeated zero-backlog observations and then still waits through stabilization and cooldown
- transient zero-backlog dips do not trigger removal when executor activity is still above the low-pressure threshold
- the operator-facing decision text stays explicit about why scale-down is allowed or blocked
- repo tests also cover stuck offload, stage-specific retry or timeout reasons, stalled post-removal drain, restart-safe resume of blocked prepare or settle work, safe re-establishment after pod churn, and clean pause or resume behavior when rollout, TLS, hibernation, or restore precedence interrupts autoscaling intent

What they do not prove:

- a runtime-injected stalled offload or drain failure harness on kind in this slice
- multi-node or bulk scale-down remediation beyond the one-step controller-owned path

## Bounded Restore Workflow Proof

The current focused bounded-restore workflow command is:

- `make kind-platform-managed-restore-fast-e2e`
- `make kind-platform-managed-restore-fast-e2e-reuse`

What it proves:

- `charts/nifi-platform` can be used as the product-facing install and reinstall path for the restore exercise
- a control-plane bundle exported by `hack/export-control-plane-backup.sh` is sufficient to drive `hack/recover-control-plane-backup.sh` for a working platform reinstall
- deleting the NiFi PVCs does not block recovery of the declarative platform layer
- the restored Flow Registry Client catalog can be used to recreate the runtime client and reconnect to the registry-backed flow source
- the restored Parameter Context config is reconciled back into NiFi through the bounded runtime-managed bootstrap
- the restored bounded flow-selection catalog can be used to identify the selected registry-backed flow and intended target process group
- a functional imported flow configuration can be rebuilt after reinstall by importing the registry-backed snapshot and attaching the reconciled Parameter Context

What it does not prove:

- queue, content, database, or provenance repository replay
- full NiFi internal runtime-state recovery
- controller-managed flow sync or generic runtime-object restore
- historical version replay beyond the current `version=latest` focused restore path

## Customer Meaning of Support Levels

Use the categories in [Compatibility](compatibility.md):

- `Focused-runtime-proven` means the feature is exercised in focused runtime workflows in this repository
- `Prepared / render-validated` means the shape is intentionally documented and rendered, but the repo does not claim runtime proof yet
- `Production-proven` is reserved for broader runtime proof than the current focused kind baseline

## Validation Used for Documentation Consistency

Customer-facing docs should stay aligned with:

- `go test ./...`
- `helm lint charts/nifi`
- `helm lint charts/nifi-platform`
- `bash -n hack/export-control-plane-backup.sh`
- `bash -n hack/recover-control-plane-backup.sh`
- `bash -n hack/prove-parameter-contexts-runtime.sh`
- `bash -n hack/kind-parameter-contexts-runtime-e2e.sh`
- `bash -n hack/prove-versioned-flow-import-runtime.sh`
- `bash -n hack/kind-platform-managed-versioned-flow-import-e2e.sh`
- `bash -n hack/prove-bounded-flow-config-restore.sh`
- `bash -n hack/kind-platform-managed-restore-e2e.sh`
- `helm template` for the standard chart install paths
- `helm template` for the Parameter Context example overlays when Parameter Context docs or values change
- `helm template` for the versioned-flow import example overlay when flow docs or values change
- `helm template` for the bounded platform restore overlay when DR docs or flow-recovery docs change
- `helm template` for optional trust-manager overlays when trust-manager docs or values change
- `jq empty` for any added starter Grafana dashboard JSON
- focused checks for the feature being documented
- focused auth and exposure render checks for OIDC internal, OIDC external URL, AKS managed, and OpenShift managed overlays when auth docs change

Documentation-only operations packaging does not require a heavier runtime gate unless it also changes shared lifecycle or e2e behavior.

## Metrics Runtime Proof Matrix

The current focused metrics runtime command is:

- `make kind-metrics-fast-e2e`
- `make kind-metrics-native-api-trust-manager-fast-e2e`
- `make kind-metrics-exporter-trust-manager-fast-e2e`
- `make kind-metrics-site-to-site-fast-e2e`
- `make kind-site-to-site-status-fast-e2e`
- `make kind-site-to-site-provenance-fast-e2e`

That matrix runs:

- `make kind-metrics-native-api-fast-e2e`
- `make kind-metrics-exporter-fast-e2e`

What it proves for `nativeApi`:

- the metrics-enabled platform overlay renders and applies
- the dedicated metrics `Service` and named `ServiceMonitor` resources exist with the expected TLS and auth wiring
- the machine-auth Secret and CA Secret contract works with operator-provided material
- the secured NiFi flow metrics endpoint can be scraped live end to end
- the recommended default shape, dedicated metrics `Service` plus multiple named scrape profiles, stays green on kind
- the optional trust-manager overlay can distribute the CA bundle that `nativeApi` consumes for the secured scrape path
- trust updates propagate from the workload TLS Secret through the mirrored trust-manager source Secret, Bundle target, and mounted probe consumer path

What it proves for `exporter`:

- the exporter overlay renders and applies
- the exporter `Deployment`, metrics `Service`, and `ServiceMonitor` exist with the expected ports, selectors, and scrape endpoint wiring
- the same machine-auth Secret and CA Secret contract is mounted and consumed correctly
- the exporter pod can directly reach the secured `/nifi-api/flow/metrics/prometheus` source path with the mounted machine-auth and CA material
- the exporter pod can also reach the already-implemented supplemental `/nifi-api/flow/status` source path
- Prometheus can scrape the exporter `/metrics` endpoint live end to end
- the exporter `/metrics` endpoint republishes live NiFi metric families from the secured `/nifi-api/flow/metrics/prometheus` source
- the exporter `/metrics` endpoint also appends selected controller-status gauges derived from `/nifi-api/flow/status`
- exporter self-diagnostics report successful refresh for both upstream sources during the scrape
- the exporter readiness probe tracks upstream secured-scrape health instead of only local process health
- the exporter recovers after mounted auth Secret rotation without restarting the exporter pod
- the focused proof uses a freshly minted token written into the referenced Secret; long-lived rotation remains operator-owned

What `make kind-metrics-exporter-trust-manager-fast-e2e` proves in addition:

- the trust-manager `Bundle` and mirrored source Secret render and reconcile through the product-facing platform chart path
- the exporter consumes the trust-manager-distributed CA bundle from the expected mount path
- the secured exporter upstream scrape succeeds with that distributed trust material instead of a manually created CA Secret
- exporter `/metrics`, `Service`, and `ServiceMonitor` stay healthy in the trust-manager-backed configuration

What `make kind-metrics-site-to-site-fast-e2e` proves:

- the typed site-to-site metrics overlay renders and applies through `charts/nifi-platform`
- the app chart mounts the bounded Site-to-Site bootstrap config into the NiFi pod
- pod `-0` bootstraps exactly one `SiteToSiteMetricsReportingTask`
- pod `-0` bootstraps exactly one `StandardRestrictedSSLContextService` when secure site-to-site transport is configured
- the reporting task reaches `RUNNING` state with the expected typed destination, transport, and format properties
- the SSL context service reaches `ENABLED` state with the expected keystore and truststore wiring
- a proof-only receiver NiFi harness comes up on kind with the expected public input port and minimal downstream processor
- the sender authenticates to that receiver over secure Site-to-Site as documented by the typed Secret/TLS contract
- live metrics delivery is observed on the receiver side through processor status, not just sender-side object state
- the feature remains chart-scoped and does not add controller-owned Site-to-Site orchestration

What `make kind-site-to-site-status-fast-e2e` proves:

- the typed site-to-site status overlay renders and applies through `charts/nifi-platform`
- the app chart mounts the bounded Site-to-Site status bootstrap config into the NiFi pod
- pod `-0` bootstraps exactly one `SiteToSiteStatusReportingTask`
- pod `-0` bootstraps exactly one `StandardRestrictedSSLContextService` when secure site-to-site transport is configured
- the reporting task reaches `RUNNING` state with the expected typed destination and transport properties
- the SSL context service reaches `ENABLED` state with the expected keystore and truststore wiring
- a proof-only receiver NiFi harness comes up on kind with the expected public input port and minimal downstream processor
- the sender authenticates to that receiver over secure Site-to-Site as documented by the typed Secret/TLS contract
- live status delivery is observed on the receiver side through processor status, not just sender-side object state
- the feature remains chart-scoped and does not add controller-owned Site-to-Site orchestration

What `make kind-site-to-site-provenance-fast-e2e` proves:

- the typed site-to-site provenance overlay renders and applies through `charts/nifi-platform`
- the app chart mounts the bounded Site-to-Site provenance bootstrap config into the NiFi pod
- pod `-0` bootstraps exactly one `SiteToSiteProvenanceReportingTask`
- pod `-0` bootstraps exactly one `StandardRestrictedSSLContextService` when secure site-to-site transport is configured
- the reporting task reaches `RUNNING` state with the expected typed destination, transport, and provenance cursor properties
- the SSL context service reaches `ENABLED` state with the expected keystore and truststore wiring
- a proof-only receiver NiFi harness comes up on kind with the expected public input port and minimal downstream processor
- the sender authenticates to that receiver over secure Site-to-Site as documented by the typed Secret/TLS contract
- live provenance delivery is observed on the receiver side through processor status, not just sender-side object state
- the feature remains chart-scoped and does not add controller-owned Site-to-Site orchestration

Current honest limit:

- `nativeApi` runtime proof still covers the flow Prometheus endpoint only
- exporter runtime proof adds a second secured endpoint, `/nifi-api/flow/status`, but not a JVM or system-diagnostics family
- the second native scrape profile is still a cadence variant of the same flow endpoint
- exporter remains optional and experimental even with the stronger runtime gate
- trust-manager-backed exporter proof currently covers PEM bundle distribution only; additional Bundle output formats are still future work for exporter mode
- `siteToSite` proof is intentionally bounded to the typed sender path plus a proof-only receiver harness on kind
- that proof now also checks the declared secure receiver identity and the minimum receiver-side policy bindings needed for delivery
- `siteToSiteStatus` proof is intentionally bounded to the typed sender path plus that same proof-only receiver harness on kind
- `siteToSiteProvenance` proof is intentionally bounded to the typed sender path plus that same proof-only receiver harness on kind
- Parameter Context support is intentionally limited to product-owned create/update/delete plus direct root-child attachment; it does not claim arbitrary graph mutation or Parameter Provider reconciliation
- versioned-flow import support is intentionally limited to restart-scoped bounded import creation, bounded source verification, and optional direct Parameter Context attachment; automatic client creation, ongoing sync, and broader process-group mutation remain out of scope
- destination receiver topology and destination-side policy lifecycle remain operator-owned outside that focused proof harness
- the current receiver harness still uses a proof-only local admin path to seed those bindings on kind
- proxy-controller-service wiring, non-Ambari record-writer ownership, broader status-task tuning, and broader provenance event-selection or batching controls remain future work for Site-to-Site typed exports

## Current Conservative Boundaries

- the repo does not yet claim a production-proven cloud runtime matrix
- AKS and OpenShift remain conservative until real-cluster proof is recorded
- experimental features stay explicitly marked experimental even when focused runtime proof exists
