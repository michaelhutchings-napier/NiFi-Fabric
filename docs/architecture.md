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
- metrics Services and ServiceMonitors
- prepared Flow Registry Client catalog files

### NiFi owns

- NiFi-native clustering behavior
- NiFi-native auth provider behavior
- NiFi-native API and runtime signals

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
- chart-owned Services and ServiceMonitors
- provider-agnostic machine-auth Secret contract

Experimental or prepared paths:

- `exporter` is experimental
- `siteToSite` is prepared-only

## Install Architecture

Standard customer path:

- one Helm release with `charts/nifi-platform`

Secondary paths:

- standalone `charts/nifi`
- advanced manual assembly for platform teams

A separate kustomize product install surface is not shipped in this slice.
