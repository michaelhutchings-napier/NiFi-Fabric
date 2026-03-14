# Observability and Metrics

Metrics are a first-class subsystem in NiFi-Fabric.

## What This Feature Does

The chart owns the metrics Services, ServiceMonitors, and machine-auth Secret references used for secured metrics scraping.

Supported modes today:

- `nativeApi`
- `exporter`
- `siteToSite` (prepared-only)

## Primary Metrics Path

`nativeApi` is the primary supported mode.

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

`exporter` is a supported secondary path.

It provides:

- a small companion exporter deployment
- a clean `/metrics` endpoint for Prometheus
- reuse of the same provider-agnostic machine-auth contract
- optional supplemental controller-status gauges derived from `/nifi-api/flow/status`

Current scope:

- flow Prometheus metrics from `/nifi-api/flow/metrics/prometheus`
- selected controller-status gauges from `/nifi-api/flow/status`
- one chart-owned exporter `Deployment`, `Service`, and `ServiceMonitor`
- upstream-aware readiness for the secured source scrape
- mounted auth Secret reread on each scrape, so credential rotation does not require an exporter pod restart

Use exporter when:

- your Prometheus environment expects a clean cluster-local `/metrics` endpoint
- you want NiFi auth and TLS handling isolated behind one chart-owned companion endpoint
- the current bounded exporter scope is enough for your deployment

## Site-to-Site Mode

`siteToSite` is prepared-only.

The chart now validates a real operator-facing contract for a future site-to-site reporting-task path, but it does not currently manage reporting tasks or the receiver pipeline.

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

Operator-owned destination assumptions:

- a receiver service already exists at `destination.url`
- the receiver exposes the named input port
- any destination-side auth and TLS trust material already exist
- the receiver expects the configured transport and format

Why runtime support is still withheld:

- NiFi reporting tasks are internal NiFi runtime objects, not normal Kubernetes resources
- a real runtime path would require this repo to own at least one `SiteToSiteMetricsReportingTask`
- destination TLS would also require bounded ownership of an SSL Context Service reference inside NiFi
- any future non-Ambari output format would require Record Writer controller-service ownership inside NiFi
- that moves beyond the current chart-owned metrics boundary and toward generic NiFi internal object management

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

Current source options:

- trust-manager source Secrets or ConfigMaps already exist in trust-manager's configured trust namespace
- or the platform chart mirrors the workload TLS `ca.crt` into a trust-manager source Secret for you

Focused kind proof can mint a fresh NiFi access token into the referenced Secret. Production deployments still need operator-owned credential rotation or a non-expiring machine credential source.

## Support Level

- `nativeApi`: primary supported path
- `exporter`: supported secondary path
- `siteToSite`: prepared-only
- trust-manager bundle consumption: optional supported complement to `nativeApi` and `exporter`, not a separate metrics mode

## Runtime Proof

Focused live proof is available through:

- `make kind-metrics-native-api-fast-e2e`
- `make kind-metrics-native-api-trust-manager-fast-e2e`
- `make kind-metrics-exporter-fast-e2e`
- `make kind-metrics-fast-e2e`

Current live scope:

- secured flow metrics are runtime-proven for `nativeApi`
- trust-manager-backed CA bundle consumption is runtime-proven for `nativeApi`
- exporter `/metrics` is runtime-proven with the secured flow Prometheus endpoint as its primary source
- exporter `/metrics` is also runtime-proven with selected controller-status gauges derived from `/nifi-api/flow/status`
- exporter readiness is runtime-proven against the secured upstream source instead of only local process health
- exporter recovery after mounted auth Secret rotation is runtime-proven without restarting the exporter pod
- exporter-specific trust-manager-backed runtime proof is still future work
- JVM or system-diagnostics metrics are not yet runtime-proven
