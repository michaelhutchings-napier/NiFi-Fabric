# Observability and Metrics

Metrics are a first-class subsystem in NiFi-Fabric.

This page also covers the typed Site-to-Site status and provenance export capabilities because they live in the same bounded observability family even though they are not metrics modes.

## What This Feature Does

`charts/nifi` owns the metrics runtime resources:

- chart-managed metrics `Service` resources
- chart-managed `ServiceMonitor` resources
- the optional exporter `Deployment`
- machine-auth Secret references and CA references for secured scraping

`charts/nifi-platform` remains the standard product install path, but exporter runtime logic stays app-chart scoped. The controller does not own metrics orchestration.

Supported metrics modes today:

- `nativeApi`
- `exporter`
- `siteToSite`

Additional typed observability export:

- `observability.siteToSiteStatus`
- `observability.siteToSiteProvenance`

## Primary Metrics Path

`nativeApi` is the primary production-ready metrics path.

Use it by default unless you have a specific reason to prefer the exporter shape.

It provides:

- secured NiFi API scraping
- a dedicated chart-managed metrics `Service`
- chart-managed `ServiceMonitor` resources
- multiple named scrape profiles
- provider-agnostic machine-auth Secret references

Recommended operator inputs:

- a machine-auth Secret referenced by `observability.metrics.nativeApi.machineAuth.secretRef`
- a CA Secret or ConfigMap referenced by `observability.metrics.nativeApi.tlsConfig.ca.*`
- one or more named scrape profiles under `observability.metrics.nativeApi.endpoints`

## Exporter Mode

`exporter` remains an optional experimental secondary path.

It provides:

- a small companion exporter deployment
- a clean cluster-local `/metrics` endpoint for Prometheus
- reuse of the same provider-agnostic machine-auth contract
- optional supplemental controller-status gauges derived from `/nifi-api/flow/status`

Current bounded scope:

- flow Prometheus metrics from `/nifi-api/flow/metrics/prometheus`
- selected controller-status gauges from `/nifi-api/flow/status`
- one chart-owned exporter `Deployment`, `Service`, and `ServiceMonitor`
- upstream-aware readiness for the secured source scrape
- mounted auth Secret reread on each scrape, so credential rotation does not require an exporter pod restart

Use exporter when:

- your Prometheus environment expects a clean cluster-local `/metrics` endpoint
- you want NiFi auth and TLS handling isolated behind one chart-owned companion endpoint
- the current bounded exporter scope is enough for your deployment

Keep the support boundary in mind:

- `nativeApi` remains the recommended production path
- exporter proof is deeper now, but the mode still stays experimental
- metrics auth stays machine-oriented, not human-login-oriented
- no controller-owned metrics lifecycle or orchestration is introduced

## Site-to-Site Metrics Mode

`siteToSite` is now a typed, bounded Site-to-Site metrics export capability.

It is intentionally not a generic Reporting Task, Controller Service, or NiFi runtime-object framework.

Current typed public contract:

- `destination.url`
- `destination.inputPortName`
- `auth.type`
- `auth.authorizedIdentity` for secure Site-to-Site receiver authorization as an RFC2253-style X.509 subject string
- `auth.secretRef.*` when `auth.type=secretRef`
- `source.applicationId`, `source.hostname`, and `source.instanceUrl`
- `transport.protocol`, `transport.communicationsTimeout`, and `transport.compressEvents`
- `format.type` and `format.includeNullValues`

What the app chart owns under the hood:

- one `SiteToSiteMetricsReportingTask`
- one `StandardRestrictedSSLContextService` when secure transport is used

What the app chart does not own:

- generic Reporting Task APIs
- generic Controller Service APIs
- generic NiFi runtime-object management
- destination receiver topology
- destination receiver-side user and policy lifecycle
- proxy-controller-service wiring
- non-Ambari record-writer ownership

Current validation and runtime boundary:

- destination URL must be present and start with `http://` or `https://`
- input port name must be present
- `auth.type` must be one of `none`, `workloadTLS`, or `secretRef`
- `auth.authorizedIdentity` is required for secure Site-to-Site modes and must stay empty for `auth.type=none`
- `https://` destinations require `workloadTLS` or `secretRef`
- `http://` destinations require `auth.type=none`
- `auth.secretRef.name` is required when `auth.type=secretRef`
- `auth.secretRef.keystoreKey`, `auth.secretRef.keystorePasswordKey`, `auth.secretRef.truststoreKey`, and `auth.secretRef.truststorePasswordKey` must stay populated when `auth.type=secretRef`
- transport protocol must be one of `RAW` or `HTTP`
- format is currently constrained to `AmbariFormat`
- the current typed bootstrap requires `auth.mode=singleUser` for local NiFi API management during object reconciliation

Receiver-side requirement for secure modes:

- the destination receiver must trust the presented client certificate chain
- the destination receiver must authorize `auth.authorizedIdentity`
- that identity needs `/controller` read
- that identity needs `/site-to-site` read
- that identity needs write on the destination input port selected by `destination.inputPortName`

Ownership rule:

- the platform owns only the specific Site-to-Site metrics export objects it creates by fixed name
- manual UI edits to those objects are unsupported and will be overwritten on the next pod restart or redeploy

## Site-to-Site Status Export

`observability.siteToSiteStatus` is a second typed, bounded Site-to-Site capability.

It stays separate from `observability.metrics.mode` so existing `nativeApi`, `exporter`, and `siteToSite` metrics behavior stays unchanged unless status export is explicitly enabled.

Current typed public contract:

- `enabled`
- `destination.url`
- `destination.inputPortName`
- `auth.type`
- `auth.authorizedIdentity` for secure Site-to-Site receiver authorization as an RFC2253-style X.509 subject string
- `auth.secretRef.*` when `auth.type=secretRef`
- `source.instanceUrl`
- `transport.protocol`, `transport.communicationsTimeout`, and `transport.compressEvents`

What the app chart owns under the hood:

- one `SiteToSiteStatusReportingTask`
- one `StandardRestrictedSSLContextService` when secure transport is used

What the app chart does not own:

- generic Reporting Task APIs
- generic Controller Service APIs
- generic NiFi runtime-object management
- destination receiver topology
- destination receiver-side user and policy lifecycle
- long-lived destination credential lifecycle
- proxy-controller-service wiring

Current validation and runtime boundary:

- destination URL must be present and start with `http://` or `https://`
- input port name must be present
- `auth.type` must be one of `none`, `workloadTLS`, or `secretRef`
- `auth.authorizedIdentity` is required for secure Site-to-Site modes and must stay empty for `auth.type=none`
- `https://` destinations require `workloadTLS` or `secretRef`
- `http://` destinations require `auth.type=none`
- `auth.secretRef.name` is required when `auth.type=secretRef`
- `auth.secretRef.keystoreKey`, `auth.secretRef.keystorePasswordKey`, `auth.secretRef.truststoreKey`, and `auth.secretRef.truststorePasswordKey` must stay populated when `auth.type=secretRef`
- transport protocol must be one of `RAW` or `HTTP`
- if `source.instanceUrl` is set, it must start with `http://` or `https://`
- the current typed bootstrap requires `auth.mode=singleUser` for local NiFi API management during object reconciliation

Fixed internal defaults for this typed feature:

- NiFi `Platform` is fixed to `nifi`
- status payloads are sent as JSON without introducing Record Writer ownership
- batch size is fixed to `1000`
- null values stay disabled by default
- component type and name filters stay fixed to the built-in all-components defaults

How it differs from Site-to-Site metrics export:

- metrics export manages `SiteToSiteMetricsReportingTask` and keeps `AmbariFormat` explicit in the public API
- status export manages `SiteToSiteStatusReportingTask` and keeps JSON status payload shape internal to the bounded implementation
- metrics export exposes source identity hints and format knobs because that task needs them
- status export keeps platform, batching, and filters internal so we do not expand into a generic reporting-task surface

Ownership rule:

- the platform owns only the specific Site-to-Site status export objects it creates by fixed name
- manual UI edits to those objects are unsupported and will be overwritten on the next pod restart or redeploy

## Site-to-Site Provenance Export

`observability.siteToSiteProvenance` is a third typed, bounded Site-to-Site capability.

It stays separate from `observability.metrics.mode` and from `observability.siteToSiteStatus` so existing `nativeApi`, `exporter`, `siteToSite` metrics, and status-export behavior stays unchanged unless provenance export is explicitly enabled.

Current typed public contract:

- `enabled`
- `destination.url`
- `destination.inputPortName`
- `auth.type`
- `auth.authorizedIdentity` for secure Site-to-Site receiver authorization as an RFC2253-style X.509 subject string
- `auth.secretRef.*` when `auth.type=secretRef`
- `source.instanceUrl`
- `transport.protocol`, `transport.communicationsTimeout`, and `transport.compressEvents`
- `provenance.startPosition`

What the app chart owns under the hood:

- one `SiteToSiteProvenanceReportingTask`
- one `StandardRestrictedSSLContextService` when secure transport is used

What the app chart does not own:

- generic Reporting Task APIs
- generic Controller Service APIs
- generic NiFi runtime-object management
- destination receiver topology
- destination receiver-side user and policy lifecycle
- long-lived destination credential lifecycle
- downstream provenance processing
- proxy-controller-service wiring

Current validation and runtime boundary:

- destination URL must be present and start with `http://` or `https://`
- input port name must be present
- `auth.type` must be one of `none`, `workloadTLS`, or `secretRef`
- `auth.authorizedIdentity` is required for secure Site-to-Site modes and must stay empty for `auth.type=none`
- `https://` destinations require `workloadTLS` or `secretRef`
- `http://` destinations require `auth.type=none`
- `auth.secretRef.name` is required when `auth.type=secretRef`
- `auth.secretRef.keystoreKey`, `auth.secretRef.keystorePasswordKey`, `auth.secretRef.truststoreKey`, and `auth.secretRef.truststorePasswordKey` must stay populated when `auth.type=secretRef`
- transport protocol must be one of `RAW` or `HTTP`
- if `source.instanceUrl` is set, it must start with `http://` or `https://`
- `provenance.startPosition` must be one of `beginningOfStream` or `endOfStream`
- the current typed bootstrap requires `auth.mode=singleUser` for local NiFi API management during object reconciliation

Fixed internal defaults for this typed feature:

- NiFi `Platform` is fixed to `nifi`
- batch size is fixed to `1000`
- the reporting task schedule is fixed to `1 min`
- only the initial cursor behavior is public; broader provenance event-selection and batching controls stay out of scope

How it differs from the other typed Site-to-Site paths:

- metrics export manages `SiteToSiteMetricsReportingTask` and keeps metrics format and source identity hints explicit
- status export manages `SiteToSiteStatusReportingTask` and keeps JSON payload shape, filters, and batching internal
- provenance export manages `SiteToSiteProvenanceReportingTask` and exposes only one provenance-specific knob, `startPosition`, for honest initial cursor control

Ownership rule:

- the platform owns only the specific Site-to-Site provenance export objects it creates by fixed name
- manual UI edits to those objects are unsupported and will be overwritten on the next pod restart or redeploy

## Machine-Auth Bootstrap

The metrics auth contract remains provider-agnostic and distinct from human login flows.

What is automated now:

- `hack/bootstrap-metrics-machine-auth.sh` can create the metrics auth Secret
- it can optionally derive the metrics CA Secret from `Secret/nifi-tls`
- it can use a pre-minted token or mint a NiFi access token from an existing machine credential already accepted by NiFi

What remains operator-provided:

- the machine principal itself
- IdP-side provisioning
- credential issuance and rotation policy
- Secret rotation and renewal workflow
- any trust-manager installation and any Secret-target permissions required by that installation

## Optional trust-manager CA Bundles

`nativeApi` remains the primary metrics path. Optional trust-manager integration can make the CA side of that path easier when you already operate trust-manager in the cluster.

What it does today:

- `charts/nifi-platform` can render a trust-manager `Bundle`
- the Bundle can target a ConfigMap or a Secret in the NiFi release namespace
- optional PKCS12 and JKS additional formats can be rendered for downstream consumers that need them
- `nativeApi` can reference that bundle instead of a manually created CA Secret
- `exporter` can reference the same bundle for its secured upstream NiFi scrape
- the platform chart can mirror the workload TLS `ca.crt` into a trust-manager source Secret automatically when you enable `trustManager.mirrorTLSSecret`

What it does not do:

- install trust-manager
- provision machine-auth credentials
- turn trust-manager into a second TLS lifecycle engine
- automatically wire non-PEM additional formats into app consumers

Focused kind proof can mint a fresh NiFi access token into the referenced Secret. Production deployments still need operator-owned credential rotation or a non-expiring machine credential source.

## Support Level

- `nativeApi`: primary production-ready path
- `exporter`: optional experimental secondary path with focused runtime proof
- `siteToSite`: optional experimental typed runtime path
- `siteToSiteStatus`: optional experimental typed status-export path
- `siteToSiteProvenance`: optional experimental typed provenance-export path
- trust-manager bundle consumption: optional supported complement to `nativeApi` and `exporter`, not a separate metrics mode

## Runtime Proof

Focused live proof is available through:

- `make kind-metrics-native-api-fast-e2e`
- `make kind-metrics-native-api-trust-manager-fast-e2e`
- `make kind-metrics-exporter-fast-e2e`
- `make kind-metrics-exporter-trust-manager-fast-e2e`
- `make kind-site-to-site-provenance-fast-e2e`
- `make kind-metrics-site-to-site-fast-e2e`
- `make kind-site-to-site-status-fast-e2e`
- `make kind-metrics-fast-e2e`

What `make kind-metrics-exporter-fast-e2e` now proves live:

- the exporter overlay renders and applies through the product-facing `charts/nifi-platform` path
- the chart-owned exporter `Deployment`, metrics `Service`, and exporter `ServiceMonitor` deploy with the expected ports, selectors, and endpoints
- the operator-provided machine-auth Secret and CA Secret exist, mount into the exporter pod, and stay wired to the documented machine-oriented contract
- the exporter pod can directly reach the secured NiFi source endpoints documented for exporter mode:
  `/nifi-api/flow/metrics/prometheus`
  `/nifi-api/flow/status`
- the exporter `/metrics` endpoint is Prometheus-scrapable
- the exporter `/metrics` payload contains relayed NiFi metric families from the secured flow Prometheus source
- the exporter `/metrics` payload also contains the selected controller-status gauges derived from `/nifi-api/flow/status`
- exporter self-diagnostics report successful refresh for both upstream sources during the scrape
- exporter readiness tracks the secured upstream source instead of only local process health
- exporter recovery after mounted auth Secret rotation is runtime-proven without restarting the exporter pod

What `make kind-metrics-exporter-trust-manager-fast-e2e` now proves live:

- `charts/nifi-platform` renders and applies the trust-manager `Bundle` path together with exporter mode enabled
- the mirrored workload TLS CA reaches the trust-manager source Secret and reconciles into the expected bundle target in the NiFi namespace
- exporter upstream TLS trust comes from the trust-manager-distributed bundle instead of a manually created metrics CA Secret
- the exporter pod mounts that trust bundle at the expected consumer path and successfully uses it for the secured NiFi source scrape
- the exporter can still reach both documented secured source endpoints:
  `/nifi-api/flow/metrics/prometheus`
  `/nifi-api/flow/status`
- the exporter `/metrics` endpoint remains healthy, Prometheus-scrapable, and populated with relayed NiFi metrics plus the already-implemented supplemental flow-status gauges

What `make kind-metrics-site-to-site-fast-e2e` now proves live:

- the typed Site-to-Site overlay renders and applies through the product-facing `charts/nifi-platform` path
- the NiFi pod mounts the chart-owned Site-to-Site bootstrap config
- pod `-0` reconciles exactly one `SiteToSiteMetricsReportingTask`
- pod `-0` reconciles exactly one `StandardRestrictedSSLContextService` when secure Site-to-Site transport is configured
- the reporting task reaches `RUNNING` state with the expected destination URL, input port name, transport protocol, and `AmbariFormat`
- the generated bootstrap config preserves the expected `auth.type`, `auth.authorizedIdentity`, material references, and required receiver-side policy contract
- the SSL context service reaches `ENABLED` state with the expected keystore and truststore wiring
- a focused proof-only receiver NiFi release is bootstrapped on kind with one public input port and one minimal downstream processor
- secure Site-to-Site peer discovery succeeds against that receiver using the documented typed auth and TLS Secret contract
- the focused proof verifies that the receiver-side authorized identity exists and is bound to `/controller` read, `/site-to-site` read, and destination input-port write
- live metrics delivery reaches the real receiver and is observed from receiver-side processor status
- the feature remains chart-scoped and does not move Site-to-Site orchestration into the controller

What `make kind-site-to-site-status-fast-e2e` now proves live:

- the typed Site-to-Site status overlay renders and applies through the product-facing `charts/nifi-platform` path
- the NiFi pod mounts the chart-owned Site-to-Site status bootstrap config
- pod `-0` reconciles exactly one `SiteToSiteStatusReportingTask`
- pod `-0` reconciles exactly one `StandardRestrictedSSLContextService` when secure Site-to-Site transport is configured
- the reporting task reaches `RUNNING` state with the expected destination URL, input port name, transport protocol, and fixed platform value
- the generated bootstrap config preserves the expected `auth.type`, `auth.authorizedIdentity`, material references, and required receiver-side policy contract
- the SSL context service reaches `ENABLED` state with the expected keystore and truststore wiring
- a focused proof-only receiver NiFi release is bootstrapped on kind with one public input port and one minimal downstream processor
- secure Site-to-Site peer discovery succeeds against that receiver using the documented typed auth and TLS Secret contract
- the focused proof verifies that the receiver-side authorized identity exists and is bound to `/controller` read, `/site-to-site` read, and destination input-port write
- live status delivery reaches the real receiver and is observed from receiver-side processor status
- the feature remains chart-scoped and does not move Site-to-Site orchestration into the controller

What remains experimental or intentionally bounded:

- `nativeApi` remains the recommended production path
- exporter proof still centers on flow Prometheus metrics plus selected `/flow/status` gauges only
- JVM or system-diagnostics metric families are not yet runtime-proven through exporter mode
- exporter remains optional and experimental even with trust-manager-backed runtime proof
- trust-manager proof currently focuses on the mirrored workload CA to PEM bundle consumer path; additional trust-manager output formats are still not runtime-proven for exporter mode
- `siteToSite` remains optional and experimental
- `siteToSite` runtime proof uses a tightly scoped kind-only receiver harness, not a product-managed destination control plane
- `siteToSiteStatus` remains optional and experimental
- `siteToSiteStatus` runtime proof uses the same tightly scoped kind-only receiver harness, not a product-managed destination control plane
- destination receiver topology and destination-side policy lifecycle remain operator-owned outside that proof harness
- the current focused proof still uses a proof-only receiver-side local admin path to seed the minimum authz needed for delivery
- proxy-controller-service wiring, destination automation beyond the proof harness, non-Ambari record-writer ownership, and broader status-task tuning remain future work for Site-to-Site typed exports
- no controller-owned metrics orchestration is introduced by this slice

## Starter Operations Package

NiFi-Fabric now includes a small starter operations package for production-oriented adoption:

- [Operations and Troubleshooting](../operations.md)
- [Operations Dashboards](../operations/dashboards.md)
- [Operations Alerts](../operations/alerts.md)
- [Operations Runbooks](../operations/runbooks.md)
- [Grafana starter dashboard JSON](../../ops/grafana/nifi-fabric-starter-dashboard.json)
- [Prometheus starter alert rules YAML](../../ops/prometheus/nifi-fabric-starter-alerts.yaml)

What it covers today:

- controller lifecycle signals for rollout, TLS drift, hibernation, restore, and autoscaling
- starter alerting for the most important controller-side failure and escalation cases
- metrics subsystem guidance for `nativeApi` and optional exporter mode
- concise first-response runbooks built around `NiFiCluster` status, controller metrics, Kubernetes events, and chart-owned metrics resources

What operators still need to adapt:

- Prometheus scrape job labels and alert routing
- Grafana datasource wiring and dashboard folder placement
- any environment-specific alerts for ingress, cloud load balancers, storage, or identity systems
- any mode-specific scrape health rules for `nativeApi` targets in your Prometheus topology
