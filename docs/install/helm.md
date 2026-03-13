# Install with Helm

This is the standard customer-facing install path.

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

## After Install

Read next:

- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Observability and Metrics](../manage/observability.md)
- [Operations and Troubleshooting](../operations.md)
