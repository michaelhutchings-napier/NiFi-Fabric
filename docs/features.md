# Features

NiFi-Fabric keeps the product surface small and explicit.

## Platform Install

- `charts/nifi-platform` is the standard install path
- one Helm release installs the CRD, controller, RBAC, app chart, and `NiFiCluster`
- `charts/nifi` stays available for standalone or advanced assembly

## Thin Controller Model

- Helm owns the standard Kubernetes resources around NiFi
- the controller owns lifecycle and safety decisions Helm cannot coordinate safely
- NiFi stays responsible for NiFi-native runtime behavior

## Lifecycle Management

- managed rollout with health gates
- TLS restart policy with cert drift observation
- hibernation to zero
- restore to the previous running size
- explicit status and event surfaces for lifecycle state

## Autoscaling

- built-in controller-owned autoscaler is the primary autoscaling model
- `Advisory` mode provides recommendation-only guidance
- `Enforced` mode supports controller-owned scale-up
- one-step safe scale-down is available and intentionally conservative
- enforced scale-down now requires durable low-pressure evidence: repeated zero-backlog observations, low executor activity when NiFi reports thread counts, extra consecutive samples when queue evidence is incomplete, and stabilization or cooldown windows
- transient zero-backlog dips are rejected when timer-driven work is still busy, and the controller records that block reason explicitly
- direct autoscaler ownership of the NiFi `StatefulSet` is not the supported architecture

## Optional KEDA Integration

- KEDA is optional and experimental
- KEDA targets `NiFiCluster`, not the NiFi `StatefulSet`
- the controller remains the only executor of actual scale actions

## TLS and cert-manager

- external Secret TLS is supported
- cert-manager integration is supported when cert-manager already exists in the cluster
- cert-manager remains a prerequisite, not a bundled dependency
- optional trust-manager integration distributes shared CA bundles into the NiFi namespace
- optional trust-manager integration can mirror the workload TLS `ca.crt` into a trust-manager source Secret
- trust-manager `Bundle` targets can be rendered as either ConfigMaps or Secrets
- exporter mode now has focused kind proof for trust-manager-distributed upstream TLS trust
- trust-manager does not replace cert-manager or move TLS orchestration into the controller

## Authentication

- single-user mode for simple deployments
- OIDC for managed browser-facing identity
- LDAP for enterprise directory integration
- OIDC and LDAP are first-class managed auth options
- named viewer, editor, flow-version-manager, and admin bundles provide the recommended customer-facing authz path
- bounded mutable-flow authz bootstrap can seed the inherited root-canvas policies needed for process-group editing and process-group-level version-control actions
- richer OIDC group-claims policy seeding is supported in the chart, with current kind browser-flow proof still being hardened conservatively

## Flow Registry Clients

- Git-based Flow Registry Clients are the supported modern direction
- GitHub, GitLab, and Bitbucket paths have focused runtime proof on NiFi `2.8.0`
- GitHub also has a focused end-to-end save-to-registry workflow proof on NiFi `2.8.0`
- the workflow proof is user-driven through the NiFi API; it does not introduce controller-managed flow deployment or synchronization
- Azure DevOps remains prepared and render-validated

## Parameter Contexts

- Parameter Context support is available as an optional typed runtime-managed feature under `parameterContexts.*`
- the bounded public surface covers named contexts, inline non-sensitive values, sensitive Kubernetes Secret references, small external Parameter Provider references, and optional direct root-child attachment targets
- the chart creates, updates, deletes, and attaches only the declared Parameter Contexts it owns in NiFi
- manual UI edits to owned contexts are live-reconciled back to the declared bounded state; undeclared contexts remain operator-owned
- providerRefs stay reference-only, and the chart does not create Parameter Providers
- focused kind proof covers declared context creation, live update without pod replacement, deletion of removed owned contexts, sensitive Secret-backed values, and bounded direct root-child attachment without widening into arbitrary flow graph editing or generic runtime-object management

## Flows

- bounded versioned-flow import is available as an optional typed runtime-managed feature under `versionedFlowImports.*`
- the public surface is intentionally small and limited to registry client selection, bucket, flow name, one selected version identifier or `latest`, one intended root-child import target name, and optional direct Parameter Context attachment
- the chart imports only the declared root-child process group instances it owns and reconciles them live on pod `-0`
- the chart attaches or updates only the selected registry-backed version for those owned process groups, records explicit ownership in the imported process-group comments, and does not write new versions back to the registry
- the selected live Flow Registry Client must already exist in NiFi; the feature reuses that client instead of broadening into generic registry-client management
- focused kind proof now covers real import of a selected registry-backed flow, resulting process-group creation, version-control state, live declared version change without pod replacement, and bounded Parameter Context attachment on the platform install path
- `latest` stays bounded: it is resolved during creation or declared-change reconcile and then pinned to the owned import until the declaration changes again
- arbitrary process-group mutation, controller-managed ongoing sync, and flow CRDs remain out of scope

## Observability

- native API metrics are the primary supported metrics mode
- exporter metrics mode is an optional experimental secondary path for clean `/metrics` scraping
- exporter live proof stays chart-scoped: a companion `Deployment`, `Service`, and `ServiceMonitor`, secured upstream reachability, and a Prometheus-scrapable `/metrics` endpoint
- exporter trust-manager live proof now covers Bundle reconciliation, mounted trust presence, and successful secured upstream reachability through the distributed bundle
- site-to-site metrics export is now an optional typed runtime path built around one SiteToSiteMetricsReportingTask use case, not a generic NiFi runtime-object API
- the typed site-to-site contract now makes the secure receiver-authorized identity explicit instead of leaving the destination-side auth requirement implicit
- site-to-site status export is an additional optional typed runtime path built around one SiteToSiteStatusReportingTask use case, not a generic reporting-task framework
- the status-export API stays separate from `observability.metrics.mode` so current `nativeApi`, `exporter`, and site-to-site metrics behavior are unaffected unless status export is explicitly enabled
- site-to-site provenance export is a third optional typed runtime path built around one `SiteToSiteProvenanceReportingTask`, not a generic reporting-task framework
- the provenance-export API stays separate from metrics and status so current `nativeApi`, `exporter`, site-to-site metrics, and site-to-site status behavior are unaffected unless provenance export is explicitly enabled
- focused kind proof now covers real sender-to-receiver delivery, receiver-side policy binding checks, and bounded proof-harness bootstrap while keeping destination ownership out of the product API
- machine-auth metrics credentials use a provider-agnostic Secret contract
- optional trust-manager bundle consumption can simplify CA trust for metrics and outbound NiFi TLS clients
- optional PKCS12 and JKS trust-manager outputs can be rendered for downstream consumers that need them
- starter operations assets now include one dashboard, one alert rules file, and concise runbooks for the main platform failure domains
- those operations assets are intentionally starter-level and must still be adapted to each environment's Prometheus, Grafana, and incident-routing setup

## Environment Scope

- kind is the current runtime proof baseline in this repository
- AKS is the primary target environment
- OpenShift is supported as a prepared secondary target
- current AKS and OpenShift claims remain render, overlay, and docs validation only unless a real cluster is explicitly exercised
- environment-specific claims remain conservative until runtime proof is recorded
