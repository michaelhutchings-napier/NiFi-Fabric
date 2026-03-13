# Observability and Metrics

Metrics are a first-class subsystem in NiFi-Fabric.

## What This Feature Does

The chart owns the metrics Services, ServiceMonitors, and machine-auth Secret references used for secured metrics scraping.

Supported modes today:

- `nativeApi`
- `exporter` (experimental)
- `siteToSite` (prepared-only)

## Primary Metrics Path

`nativeApi` is the primary supported mode.

It provides:

- secured NiFi API scraping
- chart-managed Services and ServiceMonitors
- multiple named scrape profiles
- provider-agnostic machine-auth Secret references

## Exporter Mode

`exporter` is experimental.

It provides:

- a small companion exporter deployment
- a clean `/metrics` endpoint for Prometheus
- reuse of the same provider-agnostic machine-auth contract
- optional supplemental controller-status gauges derived from `/nifi-api/flow/status`

Current scope:

- flow Prometheus metrics from `/nifi-api/flow/metrics/prometheus`
- selected controller-status gauges from `/nifi-api/flow/status`
- one chart-owned exporter `Deployment`, `Service`, and `ServiceMonitor`

## Site-to-Site Mode

`siteToSite` is prepared-only.

The chart exposes a configuration contract for a future site-to-site reporting-task path, but it does not currently manage reporting tasks or the receiver pipeline. It stays prepared-only because a real implementation would require NiFi reporting-task lifecycle ownership plus explicit destination and input-port assumptions that this repo does not manage today.

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

Focused kind proof can mint a fresh NiFi access token into the referenced Secret. Production deployments still need operator-owned credential rotation or a non-expiring machine credential source.

## Support Level

- `nativeApi`: primary supported path
- `exporter`: experimental
- `siteToSite`: prepared-only

## Runtime Proof

Focused live proof is available through:

- `make kind-metrics-native-api-fast-e2e`
- `make kind-metrics-exporter-fast-e2e`
- `make kind-metrics-fast-e2e`

Current live scope:

- secured flow metrics are runtime-proven for `nativeApi`
- exporter `/metrics` is runtime-proven with the secured flow Prometheus endpoint as its primary source
- exporter `/metrics` is also runtime-proven with selected controller-status gauges derived from `/nifi-api/flow/status`
- JVM or system-diagnostics metrics are not yet runtime-proven
