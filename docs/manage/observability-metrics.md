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
- secured scraping through machine-auth and CA references

## Exporter Metrics

Use `exporter` when your Prometheus environment prefers a dedicated `/metrics` endpoint.

This is an optional secondary path, not the primary recommendation.

The supported exporter contract preserves the upstream NiFi Prometheus metric families on the exporter endpoint and adds controller-status gauges from `/nifi-api/flow/status`.

## Site-to-Site Export

NiFi-Fabric also supports Site-to-Site observability paths for:

- metrics
- status
- provenance

These are optional and should be enabled only when you want that specific delivery model.

## trust-manager

trust-manager can be used with observability when you want a shared CA bundle for secured scraping or other outbound TLS trust.

## Related Docs

- [TLS and cert-manager](tls-and-cert-manager.md)
- [Operations and Troubleshooting](../operations.md)
- [Compatibility](../compatibility.md)
