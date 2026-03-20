# Install with Helm

`charts/nifi-platform` is the standard customer-facing install path.

The standard install story is:

1. install cert-manager first
2. verify cert-manager is ready
3. create or choose the `Issuer` or `ClusterIssuer`
4. install NiFi-Fabric

The standard cert-manager-first path does not require pre-created bootstrap auth or TLS Secrets.

## Standard Install

Install cert-manager first:

```bash
helm repo add jetstack https://charts.jetstack.io --force-update
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --set crds.enabled=true \
  --version <current-cert-manager-version>
```

Then verify cert-manager is ready and create or choose the `Issuer` or `ClusterIssuer` your cluster will use for NiFi.

The standard example expects:

- `ClusterIssuer/nifi-ca`

Install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-quickstart-values.yaml
```

The install bootstraps what it needs, and cert-manager creates the final workload TLS Secret.

After install, read the generated single-user login from the release namespace:

```bash
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d; echo
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

## Local Evaluation

For local installs, `kind` is the primary documented path and the main repository baseline.

`minikube` can also be used for small local evaluation when:

- cert-manager is installed
- the cluster can pull the controller and NiFi images
- a default `StorageClass` is available for the NiFi PVCs

Use the same standard cert-manager-first Helm install on either local cluster. For the documented `kind` workflow, see [Local Kind Guide](../local-kind.md).

## Prerequisites

Before installing, make sure:

- the cluster can pull the configured controller image
- the cluster can pull the configured NiFi image
- the cluster can provision the persistent volumes requested by the NiFi chart
- cert-manager is installed
- you have created or chosen the `Issuer` or `ClusterIssuer` for the standard path

## Next Steps

- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Advanced Install Paths](advanced.md)
- [Operations and Troubleshooting](../operations.md)
