# Observability and Metrics

Observability is a first-class part of NiFi-Fabric.

## Metrics Modes

NiFi-Fabric supports:

- `nativeApi`
- `exporter`
- `siteToSite`

The primary recommended metrics path is `nativeApi`.

## Native API Metrics

Use `nativeApi` when you want the standard metrics path for NiFi-Fabric.

It provides:

- chart-managed metrics services
- chart-managed `ServiceMonitor` resources
- multiple named scrape profiles through `observability.metrics.nativeApi.endpoints[]`
- per-profile URL parameter customization through `observability.metrics.nativeApi.endpoints[].params`
- secured scraping through machine-auth and CA references

This means you can render more than one `ServiceMonitor` against `/nifi-api/flow/metrics/prometheus`, vary interval and timeout per profile, and attach Prometheus URL parameters such as `includedRegistries` or `sampleLabelValue` per profile.

When you want a shared CA distribution path instead of manually managed CA Secrets, `nativeApi` can also consume a trust-manager-provided bundle through `observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle=true`.

## Exporter Metrics

Use `exporter` when your Prometheus environment prefers a dedicated `/metrics` endpoint.

This is an optional secondary path for environments that prefer a dedicated exporter endpoint.

The exporter preserves upstream NiFi Prometheus metric families on the exporter endpoint and adds controller-status gauges from `/nifi-api/flow/status`.

## Site-to-Site Export

NiFi-Fabric also supports Site-to-Site observability paths for:

- metrics
- status
- provenance

These are optional and should be enabled only when you want that specific delivery model.

## trust-manager

trust-manager can be used with observability when you want a shared CA bundle for secured scraping or other outbound TLS trust.

Both `nativeApi` and `exporter` metrics paths can consume a trust-manager-provided bundle instead of an explicitly managed CA Secret.

## Related Docs

- [TLS and cert-manager](tls-and-cert-manager.md)
- [Operations and Troubleshooting](../operations.md)
- [Compatibility](../compatibility.md)
