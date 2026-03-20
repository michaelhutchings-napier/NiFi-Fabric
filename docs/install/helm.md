# Install with Helm

`charts/nifi-platform` is the standard customer-facing install path.

This page covers the normal managed install first. Optional and secondary paths stay separate so the default install is easy to follow.

## Standard Install

Use the managed platform chart for a one-release install:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

For a standard first install, start with:

- `examples/platform-managed-values.yaml`

For the focused OpenShift runtime-proven baseline, compose:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`

If you want cert-manager to own the workload TLS Secret, use:

- `examples/platform-managed-cert-manager-values.yaml`

## What This Installs

With the standard managed example, Helm installs:

- the `NiFiCluster` CRD
- the controller Deployment, ServiceAccount, and RBAC
- a managed `NiFiCluster` resource
- the nested `charts/nifi` workload, including the NiFi `StatefulSet`, Services, and PVC-backed storage resources

The NiFi release runs in the Helm release namespace. In the example command above, that is `nifi`.

The example values also place the controller in a separate namespace:

- release namespace: `nifi`
- controller namespace: `nifi-system`

## Prerequisites

Before installing, make sure:

- the cluster can reach the controller image configured in `charts/nifi-platform`
- the cluster can reach the NiFi image configured in `charts/nifi`
- the cluster can provision the persistent volumes requested by the NiFi chart

The remaining prerequisites depend on which install variant you choose.

### Standard Managed Example

If you use `examples/platform-managed-values.yaml`, create these Secrets in the release namespace before installing:

- `Secret/nifi-auth`
- `Secret/nifi-tls`

If you use the OpenShift managed overlay, the same Secrets are still required. You also need a controller image that OpenShift nodes can pull, because the default dev image in `examples/platform-managed-values.yaml` is only suitable for local workflows until you override it.

### Managed + Cert-Manager Example

If you use `examples/platform-managed-cert-manager-values.yaml`:

- create `Secret/nifi-auth` in the release namespace before installing
- create `Secret/nifi-tls-params` in the release namespace before installing
- install cert-manager before installing this chart
- create the referenced issuer before installing this chart

In the example overlay, the referenced issuer is:

- `ClusterIssuer/nifi-ca`

For TLS behavior and cert-manager details, see [TLS and cert-manager](../manage/tls-and-cert-manager.md).

## Who Creates What?

| Item | Standard managed example | Managed + cert-manager example |
| --- | --- | --- |
| `Secret/nifi-auth` | You create it in the release namespace before install | You create it in the release namespace before install |
| `Secret/nifi-tls` | You create it in the release namespace before install | cert-manager creates it after Helm renders the `Certificate` |
| `Secret/nifi-tls-params` | Not used by the standard example | You create it in the release namespace before install |
| cert-manager | Not used by the standard example | You install it before installing this chart |
| issuer / `ClusterIssuer` | Not used by the standard example | You create it before installing this chart |

Helm creates the platform resources and workload objects. It does not create `nifi-auth`, the external TLS Secret used by the standard example, or the cert-manager prerequisites used by the cert-manager example.

## Optional Variants

### Cert-Manager

Use this when cert-manager already exists in the cluster and you want cert-manager to own the workload TLS Secret:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-cert-manager-values.yaml
```

### OpenShift Managed Baseline

Use this for the first real OpenShift baseline through the standard product chart path:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/openshift/managed-values.yaml \
  --set controller.image.repository=<your-registry>/nifi-fabric-controller \
  --set controller.image.tag=<tag>
```

What this baseline proves:

- `charts/nifi-platform` remains the customer install surface on OpenShift
- the chart installs the CRD, controller, nested app chart, and managed `NiFiCluster` in one release
- the NiFi cluster starts securely and passes the existing internal health gate
- the controller becomes ready and manages the chart-installed `NiFiCluster`

What this baseline does not yet prove:

- Route-backed access
- OIDC or LDAP browser login through an OpenShift Route
- cert-manager on OpenShift
- the standalone app-chart path on OpenShift

The focused internal proof command is:

```bash
CONTROLLER_IMAGE_REPOSITORY=<your-registry>/nifi-fabric-controller \
CONTROLLER_IMAGE_TAG=<tag> \
make openshift-platform-managed-proof
```

### Service Mesh Profiles

Service mesh profiles are optional and secondary. They are documented separately in [Optional Service Mesh Profiles](service-mesh.md).

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
