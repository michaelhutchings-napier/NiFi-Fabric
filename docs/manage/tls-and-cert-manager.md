# TLS and cert-manager

NiFi-Fabric is TLS-first.

## What This Feature Does

NiFi-Fabric supports two TLS sourcing models:

- external Kubernetes Secret
- cert-manager-managed certificate Secret

The chart owns the workload wiring. The controller owns TLS lifecycle decisions such as observe-only, autoreload-first behavior, and restart-required escalation.

## Standard Configuration Surface

Use `charts/nifi` values under:

- `tls.mode`
- `tls.existingSecret`
- `tls.autoReload.*`
- `tls.certManager.*`

Use `charts/nifi-platform` values under:

- `nifi.tls.*`
- `cluster.restartPolicy.tlsDrift`
- `trustManager.*` for optional shared CA bundle distribution

## External Secret Mode

This is the advanced explicit-ownership path.

Explicit path:

- you create `Secret/nifi-tls` in the release namespace before install

The bounded self-signed quickstart remains available as a secondary bootstrap path, but it is not the primary recommended customer TLS story.

The Secret contract is defined by the app chart values:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- password keys
- `sensitivePropsKey` or a dedicated secret reference

## Cert-Manager Mode

cert-manager mode is supported when cert-manager already exists in the cluster.

This is the primary recommended TLS story for the standard managed install path.

What NiFi-Fabric expects:

- cert-manager is installed separately
- the issuer is already available
- the chart can render the `Certificate`
- stable password references are still provided

There are two ownership stories for the supporting inputs:

- standard cert-manager-first path: the standard install path bootstraps `nifi-auth` and `nifi-tls-params` automatically in the release namespace when needed
- explicit cert-manager path: you create `nifi-auth` and `nifi-tls-params` in the release namespace before install

In both cert-manager cases:

- Helm renders the `Certificate`
- cert-manager creates and rotates `Secret/nifi-tls`
- cert-manager and the issuer remain operator-provided prerequisites

What NiFi-Fabric does not do:

- install cert-manager for you as part of the product chart
- replace cert-manager lifecycle with controller lifecycle

## Optional trust-manager Integration

trust-manager is optional and disabled by default.

Use it when you already run trust-manager in the cluster and want a chart-managed way to distribute a shared CA bundle into the release namespace for:

- secured metrics scraping
- NiFi outbound trust such as LDAP, OIDC, or Flow Registry TLS

What this integration does:

- renders an optional trust-manager `Bundle` from `charts/nifi-platform`
- targets the NiFi release namespace only
- supports ConfigMap or Secret bundle targets, with optional PKCS12 and JKS additional formats when trust-manager is configured to produce them
- can mirror the workload TLS Secret `ca.crt` into trust-manager's trust namespace through a chart-owned bootstrap Job plus recurring CronJob
- lets `charts/nifi` consume the resulting PEM bundle for metrics TLS and optional extra trust import
- adds the generated bundle ConfigMap or Secret to the default managed restart-trigger set when NiFi imports that bundle into its runtime truststore

What it does not do:

- install trust-manager
- replace cert-manager
- move trust orchestration into the controller
- manage arbitrary NiFi internal TLS objects beyond the existing chart-owned trust bundle consumption path
- make Secret targets work unless the upstream trust-manager installation already enables and authorizes secret targets

Current source options:

- source Secrets or ConfigMaps can be operator-provided directly in trust-manager's configured trust namespace
- or `trustManager.mirrorTLSSecret.enabled=true` can mirror the workload TLS `ca.crt` into a trust-manager source Secret automatically

Focused proof:

- `make kind-platform-managed-trust-manager-fast-e2e`
- `make kind-metrics-native-api-trust-manager-fast-e2e`
- `make kind-metrics-exporter-trust-manager-fast-e2e`

What the exporter-specific trust-manager proof adds:

- a managed install path with `trustManager.enabled=true` and `observability.metrics.mode=exporter`
- live proof that the trust-manager Bundle target reaches the exporter trust mount path in the release namespace
- live proof that the exporter can use that trust bundle to reach the secured NiFi metrics source and keep `/metrics` healthy
- no change to the product architecture: cert-manager remains the TLS lifecycle engine, trust-manager remains an optional CA distribution layer, and the controller still does not orchestrate trust distribution

Current operator-controlled values:

- `trustManager.sources.*`
- `trustManager.target.*`
- `trustManager.mirrorTLSSecret.*`
- `nifi.tls.additionalTrustBundle.*`
- `nifi.trustManagerBundleRef.*` when the platform bundle target is a Secret or uses a non-default key
- `nifi.observability.metrics.nativeApi.tlsConfig.ca.*`
- `nifi.observability.metrics.exporter.source.tlsConfig.ca.*`

## Support Level

- external Secret mode: supported
- cert-manager mode: supported, with cert-manager as a prerequisite
- trust-manager integration: optional supported CA bundle distribution, with trust-manager as a prerequisite
- environment-specific proof: kind-focused today, see [Compatibility](../compatibility.md)
