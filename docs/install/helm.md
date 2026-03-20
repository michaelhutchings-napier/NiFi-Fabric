# Install with Helm

`charts/nifi-platform` is the standard customer-facing install path.

This page starts with the normal managed install, then lists optional variants and secondary paths. It does not cover local test harnesses or focused proof overlays.

## Standard Install

Use the managed platform chart for a one-release install:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

Common values files:

- `examples/platform-managed-values.yaml`: standard managed install
- `examples/platform-managed-cert-manager-values.yaml`: managed install with cert-manager-owned TLS

## What This Installs

With the standard managed example, Helm installs:

- the `NiFiCluster` CRD
- the controller Deployment, ServiceAccount, and RBAC
- a `NiFiCluster` resource for the managed NiFi deployment
- the nested `charts/nifi` workload, including the NiFi `StatefulSet`, Services, and PVC-backed storage resources

The exact namespaces and names come from your Helm values. In the example files:

- the NiFi release is installed into `nifi`
- the controller runs in `nifi-system`

## Prerequisites

### Common Prerequisites

Before installing, make sure:

- you can reach the controller image configured in `charts/nifi-platform`
- you can reach the NiFi image configured in `charts/nifi`
- the cluster can provision the persistent volumes requested by the NiFi chart

The chart creates the platform resources. It does not create your authentication Secrets, your external TLS input Secret, or optional cluster integrations such as cert-manager or a service mesh control plane.

### Standard External-Secret Path

The standard example in `examples/platform-managed-values.yaml` expects these Secrets to already exist in the NiFi namespace:

- `Secret/nifi-auth`
- `Secret/nifi-tls`

This is the default managed path shown in the main install command above.

### Cert-Manager Variant

If you use `examples/platform-managed-cert-manager-values.yaml`, these inputs change:

- `Secret/nifi-auth` must still already exist
- `Secret/nifi-tls` is created by cert-manager, not pre-created by you
- `Secret/nifi-tls-params` must already exist for the PKCS12 password and `nifi.sensitive.props.key`
- cert-manager must already be installed
- the issuer referenced by the example values must already exist

The example overlay expects:

- `ClusterIssuer/nifi-ca`

For TLS behavior and cert-manager details, see [TLS and cert-manager](../manage/tls-and-cert-manager.md).

### Optional Service Mesh Variants

Service mesh profiles are optional and documented separately so the standard install path stays focused.

If you use one of those overlays, the mesh remains a cluster prerequisite:

- Linkerd: install and operate Linkerd separately
- Istio sidecar mode: install Istio separately and enable injection only for the NiFi namespace
- Istio Ambient: install Istio Ambient separately

These overlays affect the NiFi workload only. The controller stays outside the mesh. See [Optional Service Mesh Profiles](service-mesh.md).

## Optional Install Variants

### Cert-Manager

Use this when cert-manager already exists in the cluster and you want cert-manager to own the workload TLS Secret:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-cert-manager-values.yaml
```

For optional service mesh install commands, see [Optional Service Mesh Profiles](service-mesh.md).

## Standalone Chart

If you want the reusable NiFi app chart without the managed controller path, install `charts/nifi` directly:

```bash
helm upgrade --install nifi charts/nifi \
  --namespace nifi \
  --create-namespace \
  -f examples/standalone/values.yaml
```

This is a valid secondary path, but it is not the standard product install story.

## Secondary Manifest Bundle

If you need a manifest-based workflow, use the generated bundle path in [Advanced Install Paths](advanced.md):

```bash
make render-platform-managed-bundle
kubectl apply -f dist/nifi-platform-managed-bundle.yaml
```

This bundle is rendered from `charts/nifi-platform`, so Helm remains the source of truth.

## Next Steps

- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Observability and Metrics](../manage/observability-metrics.md)
- [Operations and Troubleshooting](../operations.md)
