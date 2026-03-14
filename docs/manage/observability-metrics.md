# Observability and Metrics

Metrics are a first-class subsystem in NiFi-Fabric.

## What This Feature Does

`charts/nifi` owns the metrics runtime resources:

- chart-managed metrics `Service` resources
- chart-managed `ServiceMonitor` resources
- the optional exporter `Deployment`
- machine-auth Secret references and CA references for secured scraping

`charts/nifi-platform` remains the standard product install path, but exporter runtime logic stays app-chart scoped. The controller does not own metrics orchestration.

Supported modes today:

- `nativeApi`
- `exporter`
- `siteToSite` (prepared-only)

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

## Site-to-Site Mode

`siteToSite` is prepared-only.

The chart validates a real operator-facing contract for a future site-to-site reporting-task path, but it does not currently manage reporting tasks or the receiver pipeline.

Current prepared contract:

- `destination.url`
- `destination.inputPortName`
- `destination.auth.*`
- `destination.tls.*`
- `source.applicationId`, `source.hostname`, and `source.instanceUrl`
- `transport.protocol`, `transport.communicationsTimeout`, and `transport.compressEvents`
- `format.type` and `format.includeNullValues`

Current render-time validation:

- destination URL must be present and start with `http://` or `https://`
- input port name must be present
- receiver auth references must be internally complete when auth is enabled
- TLS CA references must be internally complete when configured
- TLS settings cannot be supplied for a plain `http://` destination
- transport protocol must be one of `RAW` or `HTTP`
- format is currently constrained to `AmbariFormat`

Why runtime support is still withheld:

- NiFi reporting tasks are internal NiFi runtime objects, not normal Kubernetes resources
- a real runtime path would require this repo to own at least one `SiteToSiteMetricsReportingTask`
- destination TLS would also require bounded ownership of an SSL Context Service reference inside NiFi
- any future non-Ambari output format would require Record Writer controller-service ownership inside NiFi

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
- `siteToSite`: prepared-only
- trust-manager bundle consumption: optional supported complement to `nativeApi` and `exporter`, not a separate metrics mode

## Runtime Proof

Focused live proof is available through:

- `make kind-metrics-native-api-fast-e2e`
- `make kind-metrics-native-api-trust-manager-fast-e2e`
- `make kind-metrics-exporter-fast-e2e`
- `make kind-metrics-exporter-trust-manager-fast-e2e`
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

What remains experimental or intentionally bounded:

- `nativeApi` remains the recommended production path
- exporter proof still centers on flow Prometheus metrics plus selected `/flow/status` gauges only
- JVM or system-diagnostics metric families are not yet runtime-proven through exporter mode
- exporter remains optional and experimental even with trust-manager-backed runtime proof
- trust-manager proof currently focuses on the mirrored workload CA to PEM bundle consumer path; additional trust-manager output formats are still not runtime-proven for exporter mode
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
