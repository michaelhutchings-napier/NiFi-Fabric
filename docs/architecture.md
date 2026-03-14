# Architecture Summary

NiFi-Fabric is built around a small, explainable split of responsibilities.

## Product Components

- `charts/nifi-platform`: standard customer install chart
- `charts/nifi`: reusable NiFi app chart
- `NiFiCluster`: thin operational API for managed mode
- controller: lifecycle and safety coordinator for the managed path

## Ownership Model

### Helm owns

- standard Kubernetes resources
- the NiFi `StatefulSet`
- Services, PVCs, ingress or Route resources
- Secret references
- cert-manager `Certificate` resources when that mode is enabled
- optional trust-manager `Bundle` resources when that mode is enabled
- metrics Services and ServiceMonitors
- prepared Flow Registry Client catalog files
- bounded authz bootstrap bundles for mutable flow work

### NiFi owns

- NiFi-native clustering behavior
- NiFi-native auth provider behavior
- NiFi-native API and runtime signals
- persisted file-provider authorization state after chart bootstrap

### The controller owns

- managed rollout sequencing
- TLS restart policy decisions
- hibernation and restore orchestration
- controller-owned autoscaling recommendations and execution
- lifecycle precedence and safety gating
- explicit status and event reporting

## One Lifecycle Control Plane

The controller remains the only executor of destructive lifecycle actions in managed mode.

That includes:

- rollout pod deletion sequencing
- hibernation and restore sequencing
- controller-owned autoscaling execution

This is why direct autoscaler ownership of the NiFi `StatefulSet` is not the product architecture.

## Autoscaling Architecture

Primary model:

- controller-owned autoscaling
- `Disabled`, `Advisory`, and `Enforced` modes
- one-step, conservative scale-down

Optional experimental extension:

- KEDA writes external intent to `NiFiCluster`
- the controller still decides whether a safe scale action should happen

## Observability Architecture

Primary metrics path:

- `observability.metrics.mode=nativeApi`
- dedicated chart-owned metrics `Service` plus named `ServiceMonitor` resources
- provider-agnostic machine-auth Secret contract
- focused live runtime proof for secured flow metrics scraping
- recommended production path for customers by default

Experimental or prepared paths:

- `exporter` is a supported secondary metrics path for environments that want a clean `/metrics` endpoint
- the exporter republishes the secured flow Prometheus endpoint and can append selected controller-status gauges from `/nifi-api/flow/status`
- the exporter keeps local liveness separate from upstream-aware readiness and rereads mounted auth material without requiring a pod restart
- `siteToSite` stays optional and is now a typed metrics-export capability instead of a generic NiFi runtime-object framework
- the public API remains bounded to one metrics use case under `observability.metrics.siteToSite`
- the app chart owns only the minimum internal NiFi objects required for that use case:
- one `SiteToSiteMetricsReportingTask`
- one `StandardRestrictedSSLContextService` when secure site-to-site transport is enabled
- no generic Reporting Task, Controller Service, or NiFi runtime-object public API is introduced
- record-writer ownership for non-Ambari formats and proxy-controller-service ownership remain future work
- destination receiver topology, input-port policy decisions, and any reverse-proxy routing assumptions remain explicit operator-owned concerns
- current runtime ownership is intentionally chart-scoped and bootstrap-scoped rather than controller-owned orchestration

Current conservative boundary:

- `nativeApi` runtime proof is still centered on the secured flow Prometheus endpoint
- exporter runtime proof adds one second secured endpoint, `/nifi-api/flow/status`, through the chart-owned exporter path
- site-to-site runtime proof is intentionally bounded to typed reporting-task and SSL-context bootstrap; full receiver-pipeline proof remains narrower than the generic site-to-site problem space
- JVM or system-diagnostics metrics are not yet runtime-proven
- machine-auth Secret bootstrap is partially automated, but machine principal provisioning and IdP write-back remain out of scope

## Trust Distribution Architecture

Primary TLS path:

- external Secret or cert-manager-issued NiFi TLS material
- chart-owned mounting and restart-trigger wiring
- controller-owned TLS drift observation and safe restart policy

Optional trust-manager extension:

- `charts/nifi-platform` can render a trust-manager `Bundle` for shared CA distribution
- the Bundle targets the NiFi release namespace only
- `charts/nifi` can consume that bundle for:
- secured metrics CA trust
- optional extra CA import into NiFi's runtime truststore for outbound trust
- the controller does not orchestrate trust bundles
- supported Bundle targets stay bounded:
- ConfigMap targets for PEM distribution
- Secret targets when the upstream trust-manager installation allows secret targets
- optional additional PKCS12 and JKS outputs when explicitly configured

Current conservative boundary:

- trust-manager is optional and disabled by default
- cert-manager remains the primary supported certificate lifecycle
- trust-manager support stays focused on CA and trust bundle distribution, not full TLS orchestration
- optional platform-owned TLS CA mirroring can copy the workload `ca.crt` into trust-manager's source namespace
- that mirroring remains chart-owned helper automation, not controller-owned trust orchestration
- trust-manager source Secrets or ConfigMaps can still be operator-provided directly in trust-manager's configured trust namespace

## Install Architecture

Standard customer path:

- one Helm release with `charts/nifi-platform`

Secondary paths:

- standalone `charts/nifi`
- advanced manual assembly for platform teams
- generated manifest bundle rendered from `charts/nifi-platform`

The secondary manifest bundle stays generated from the Helm chart at render time, so Helm remains the source of truth and kustomize-specific chart duplication is avoided.
