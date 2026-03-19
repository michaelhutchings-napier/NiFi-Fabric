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
- `examples/platform-fast-values.yaml` for smaller focused evaluations only

The shared NiFi `2.x` compatibility contract composes:

- `examples/platform-managed-values.yaml`
- `examples/platform-managed-metrics-native-values.yaml`
- `examples/platform-fast-values.yaml`
- an inline NiFi image tag selection inside the focused harness

Focused matrix command:

- `make kind-nifi-compatibility-fast-e2e`

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
