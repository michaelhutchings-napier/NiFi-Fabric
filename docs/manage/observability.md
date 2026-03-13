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

Current scope:

- flow metrics family only

## Site-to-Site Mode

`siteToSite` is prepared-only.

The chart exposes a configuration contract for a future site-to-site reporting-task path, but it does not currently manage reporting tasks or the receiver pipeline.

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
- the same flow metrics family republished on exporter `/metrics` is runtime-proven for `exporter`
- no second distinct metrics family is claimed live yet
