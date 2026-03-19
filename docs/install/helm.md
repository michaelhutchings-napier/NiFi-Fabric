# Install with Helm

This is the standard customer-facing install path.

Environment note:

- kind is the current runtime-proof baseline for this install path
- AKS and OpenShift overlays are validated through Helm rendering and docs in this slice, not by real-cluster runtime gates

## Standard Path

Use `charts/nifi-platform` for a one-release install.

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

## What This Installs

In managed mode, the platform chart installs:

- the `NiFiCluster` CRD
- the controller Deployment and RBAC
- the `charts/nifi` app chart
- a `NiFiCluster` resource that targets the chart-managed `StatefulSet`

## Prerequisites

Before install, make sure the target cluster can access:

- the controller image configured in `charts/nifi-platform`
- the NiFi image configured in `charts/nifi`

Required Secrets in the NiFi namespace:

- `Secret/nifi-tls`
- `Secret/nifi-auth`

If you use cert-manager TLS mode:

- cert-manager must already be installed in the cluster
- the issuer and supporting password Secrets must already exist

## Standard Overlay Files

Common starting overlays:

- `examples/platform-managed-values.yaml`
- `examples/platform-managed-cert-manager-values.yaml`
- `examples/platform-managed-linkerd-values.yaml` for the bounded Linkerd-compatible NiFi workload profile
- `examples/platform-managed-istio-values.yaml` for the bounded Istio sidecar-mode NiFi workload profile
- `examples/platform-managed-istio-ambient-values.yaml` for the bounded Istio Ambient NiFi workload profile
- `examples/platform-fast-values.yaml` for smaller focused evaluations only

The shared NiFi `2.x` compatibility contract composes:

- `examples/platform-managed-values.yaml`
- `examples/platform-managed-metrics-native-values.yaml`
- `examples/platform-fast-values.yaml`
- an inline NiFi image tag selection inside the focused harness

Focused matrix command:

- `make kind-nifi-compatibility-fast-e2e`

## Linkerd-Compatible Install Variant

Use this when you want the bounded supported Linkerd profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-linkerd-values.yaml
```

Boundaries of this install variant:

- it injects only the NiFi StatefulSet pods
- it does not mesh the controller
- it marks the NiFi cluster protocol and load-balance ports opaque by default
- it leaves the HTTPS port non-opaque in the documented baseline profile
- it does not introduce any mesh-specific controller behavior

Operational note:

- prefer the workload overlay over namespace-wide Linkerd injection so the controller stays mesh-agnostic and the supported profile remains explicit
- the Linkerd control plane and any mesh prerequisites remain operator-owned; this overlay only makes the NiFi workload compatible with the documented bounded profile

## Istio Sidecar-Compatible Install Variant

Use this when you want the bounded supported Istio sidecar-mode profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-istio-values.yaml
```

Boundaries of this install variant:

- it expects the operator to enable sidecar injection on the NiFi namespace only
- it injects only the NiFi StatefulSet pods
- it supports Istio sidecar mode only, not ambient mode
- it does not mesh the controller
- it enables explicit pod annotations for probe rewrite and holding NiFi startup until the sidecar proxy is ready
- it does not introduce any mesh-specific controller behavior

Operational note:

- prefer the workload overlay over namespace-wide Istio injection so the controller stays mesh-agnostic and the supported profile remains explicit
- when using the bounded supported profile, label the NiFi namespace for Istio injection and leave the controller namespace unlabeled
- the Istio control plane, namespace conventions, and any ingress or gateway resources remain operator-owned; this overlay only makes the NiFi workload compatible with the documented bounded sidecar profile

## Istio Ambient-Compatible Install Variant

Use this when you want the bounded supported Istio Ambient profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-istio-ambient-values.yaml
```

Boundaries of this install variant:

- it enrolls only the NiFi StatefulSet pods through pod-template labels
- it supports Istio Ambient L4 mode only
- it does not add sidecars, waypoint behavior, or probe-rewrite logic
- it does not mesh the controller
- it does not introduce any mesh-specific controller behavior

Operational note:

- prefer the workload overlay over namespace-wide Ambient enrollment so the controller stays mesh-agnostic and the supported profile remains explicit
- the Istio Ambient control plane, any waypoint configuration, and any ingress or gateway resources remain operator-owned; this overlay only makes the NiFi workload compatible with the documented bounded Ambient profile

## Cert-Manager Install Variant

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-cert-manager-values.yaml
```

Use this only when cert-manager is already available in the cluster.

## Standalone Chart

If you do not want the controller-managed path, install `charts/nifi` directly:

```bash
helm upgrade --install nifi charts/nifi \
  --namespace nifi \
  --create-namespace \
  -f examples/standalone/values.yaml
```

That is a valid install path, but it is not the standard product path.

## Secondary Manifest Bundle

If you need a manifest-based workflow, use the generated bundle path documented in [Advanced Install Paths](advanced.md).

That bundle is rendered from `charts/nifi-platform`, so Helm remains the source of truth even when you do not install with `helm upgrade --install`.

## After Install

Read next:

- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Observability and Metrics](../manage/observability-metrics.md)
- [Operations and Troubleshooting](../operations.md)
