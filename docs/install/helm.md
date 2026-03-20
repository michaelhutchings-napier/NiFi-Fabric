# Install with Helm

`charts/nifi-platform` is the standard customer-facing install path.

The standard install story is:

1. install cert-manager
2. verify cert-manager is ready
3. create or choose the `Issuer` or `ClusterIssuer`
4. install NiFi-Fabric

You do not need to pre-create bootstrap auth or TLS Secrets for this standard path.

## Standard Install

### Install cert-manager

Install cert-manager first:

```bash
helm repo add jetstack https://charts.jetstack.io --force-update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --version <current-cert-manager-version>
```

Then verify cert-manager is ready, create or choose the `Issuer` or `ClusterIssuer` your cluster will use for NiFi, and install NiFi-Fabric.

The standard example expects:

- `ClusterIssuer/nifi-ca`

Install the standard managed cert-manager-first path with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-quickstart-values.yaml
```

This standard path is intentionally bounded:

- managed platform install with `charts/nifi-platform`
- the standard path uses `singleUser` authentication
- cert-manager-owned workload TLS
- chart-generated bootstrap Secrets where needed

After install, read the generated single-user login from the release namespace:

```bash
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d; echo
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

## What This Installs

With the standard managed example, Helm installs:

- the `NiFiCluster` CRD
- the controller Deployment, ServiceAccount, and RBAC
- a managed `NiFiCluster` resource
- the nested `charts/nifi` workload, including the NiFi `StatefulSet`, Services, and PVC-backed storage
- the cert-manager `Certificate` for the workload TLS Secret
- the bootstrap Secrets needed for the standard single-user cert-manager-first path

The NiFi workload is installed into the Helm release namespace. In the example above, that is `nifi`.

The example values place the controller in a separate namespace:

- release namespace: `nifi`
- controller namespace: `nifi-system`

That split is only an example, not a product requirement.

## Prerequisites

Every install variant needs:

- a cluster that can pull the configured controller image
- a cluster that can pull the configured NiFi image
- storage for the PVCs requested by the NiFi chart

The standard customer path also needs these cluster prerequisites before Helm:

- cert-manager
- a chosen `Issuer` or `ClusterIssuer`

You do not pre-create `nifi-auth`, `nifi-tls`, or `nifi-tls-params` for this standard path.

## Who Creates What?

| Item | Standard cert-manager-first path | Advanced install |
| --- | --- | --- |
| `Secret/nifi-auth` | The standard install path bootstraps it automatically in the release namespace and reuses it on upgrade | You create it when your chosen auth mode needs it |
| `Secret/nifi-tls` | cert-manager creates it from the rendered `Certificate` | You create it for external-Secret TLS, or cert-manager creates it in the explicit cert-manager path |
| `Secret/nifi-tls-params` | The standard install path bootstraps it automatically in the release namespace when needed and reuses it on upgrade | You create it when using the explicit cert-manager path with Secret refs |
| cert-manager | You install it before Helm | Optional, depending on whether you choose cert-manager or external TLS |
| issuer / `ClusterIssuer` | You create it before Helm | Required only for cert-manager-based advanced installs |

Helm always creates the platform resources and workload objects. cert-manager creates the final workload TLS Secret on the standard path.

## Optional Variants

### Advanced Install

If you want operator-provided auth or TLS Secrets, use the advanced path in [Advanced Install Paths](advanced.md).

That is where explicit ownership lives for:

- existing TLS Secrets
- existing auth Secrets
- external-Secret TLS ownership
- production-style secret control
- OIDC and LDAP installs with explicit identity-provider inputs

### Optional Service Mesh Profiles

Service mesh profiles are optional and secondary. See [Optional Service Mesh Profiles](service-mesh.md).

### Standalone Chart

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
make render-platform-managed-cert-manager-bundle
kubectl apply -f dist/nifi-platform-managed-cert-manager-bundle.yaml
```

Helm remains the primary install recommendation.

## Next Steps

- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Advanced Install Paths](advanced.md)
- [Operations and Troubleshooting](../operations.md)
