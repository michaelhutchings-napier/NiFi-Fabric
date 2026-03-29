# Observability and Metrics

Observability is a first-class part of NiFi-Fabric.

Design-time flow-change audit is a separate capability and is not part of `observability.metrics`.
See [Flow-Change Audit](observability-audit.md).

## Metrics Modes

NiFi-Fabric supports:

- `nativeApi`
- `exporter`
- `siteToSite`

The primary recommended metrics path is `nativeApi`.

For `charts/nifi-platform`, the default managed recommendation is native API metrics with no reverse-proxy sidecar:

- `nifi.observability.metrics.mode=nativeApi`
- `nifi.observability.metrics.nativeApi.serviceMonitor.enabled=false` until Prometheus Operator is present

The base `charts/nifi` chart keeps native API `ServiceMonitor` rendering enabled by default for backward compatibility. Set `observability.metrics.nativeApi.serviceMonitor.enabled=false` explicitly there when you want only the dedicated metrics `Service`.

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

### ServiceMonitor Behavior

`nativeApi` always supports the dedicated metrics `Service`.

For `charts/nifi-platform`, `ServiceMonitor` creation stays explicit through `nifi.observability.metrics.nativeApi.serviceMonitor.enabled=true` so the default managed install does not assume Prometheus Operator is present.

For direct `charts/nifi` installs, native API `ServiceMonitor` rendering stays enabled by default for backward compatibility, and the same flag can disable it when needed.

That split keeps the default managed path safe on clusters that do not install Prometheus Operator.

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

- [Flow-Change Audit](observability-audit.md)
- [Operations Dashboards](../operations/dashboards.md)
- [TLS and cert-manager](tls-and-cert-manager.md)
- [Operations and Troubleshooting](../operations.md)
- [Compatibility](../compatibility.md)
