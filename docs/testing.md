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

- site-to-site metrics remains prepared-only
- AKS guidance is published but still conservative
- OpenShift guidance is published but still conservative
- Azure DevOps Flow Registry Client definitions are prepared and render-validated

## Flow Registry Workflow Proof

The current focused end-to-end workflow command is:

- `make kind-flow-registry-github-workflow-fast-e2e`
- `make kind-flow-registry-github-workflow-fast-e2e-reuse`

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
- `helm template` for the standard chart install paths
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

Current honest limit:

- `nativeApi` runtime proof still covers the flow Prometheus endpoint only
- exporter runtime proof adds a second secured endpoint, `/nifi-api/flow/status`, but not a JVM or system-diagnostics family
- the second native scrape profile is still a cadence variant of the same flow endpoint
- exporter remains optional and experimental even with the stronger runtime gate
- trust-manager-backed exporter proof currently covers PEM bundle distribution only; additional Bundle output formats are still future work for exporter mode
- `siteToSite` remains prepared-only and is intentionally excluded from the live matrix
- the site-to-site overlay is only validated at Helm render time for destination URL, input port, auth, TLS, transport, and format compatibility

## Current Conservative Boundaries

- the repo does not yet claim a production-proven cloud runtime matrix
- AKS and OpenShift remain conservative until real-cluster proof is recorded
- experimental features stay explicitly marked experimental even when focused runtime proof exists
