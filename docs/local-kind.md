# Local Kind Guide

Use this page when you want to try NiFi-Fabric locally on `kind`.

This is a local evaluation and maintainer workflow. It is useful for learning the product, checking chart changes, and reproducing issues quickly. It is not a production deployment guide.

## Best Local Starting Point

The simplest local path mirrors the standard product story:

1. create a `kind` cluster
2. install cert-manager and a local issuer
3. load the local controller and NiFi images into the cluster
4. install `charts/nifi-platform`
5. run the health check

## Prerequisites

Install these tools first:

- `kind`
- `kubectl`
- `helm`
- `docker`
- `make`
- `go` if you want to rebuild the local controller image

You also need the local images used by the default examples:

- `nifi-fabric-controller:dev`
- `apache/nifi:2.0.0`

If the local controller image is missing, build it with:

```bash
make docker-build-controller
```

## Standard Local Install

Create the cluster:

```bash
make kind-up
```

Load the images:

```bash
make kind-load-nifi-image
make kind-load-controller
```

Install cert-manager and the local bootstrap issuer:

```bash
make kind-bootstrap-cert-manager
```

Install NiFi-Fabric:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-quickstart-values.yaml
```

Check cluster health:

```bash
make kind-health
```

This local standard path does not require you to pre-create bootstrap auth or TLS Secrets.

## Useful Local Commands

Check the main workload:

```bash
kubectl -n nifi get pods,statefulset,nificluster
```

Check the controller:

```bash
kubectl -n nifi-system get deploy,pods
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Get the quickstart single-user credentials:

```bash
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.username}' | base64 -d; echo
kubectl -n nifi get secret nifi-auth -o jsonpath='{.data.password}' | base64 -d; echo
```

Remove the cluster when you are done:

```bash
make kind-down
```

## Advanced Local Paths

For local testing beyond the standard path:

- use [Advanced Install Paths](install/advanced.md) for explicit Secret ownership, OIDC, and LDAP
- use [KEDA integration](keda.md) if you want to exercise KEDA locally
- use the relevant `make kind-...` targets when you are validating a specific feature or reproducing an issue

Examples:

```bash
make kind-platform-managed-cert-manager-fast-e2e
make kind-auth-oidc-e2e
make kind-auth-ldap-e2e
make kind-keda-scale-up-fast-e2e
```

## Troubleshooting

If the cluster does not become healthy, start with:

```bash
kubectl -n nifi get pods
kubectl -n nifi get nificluster nifi -o yaml
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
kubectl -n nifi get events --sort-by=.lastTimestamp | tail -n 50
```

For broader operator guidance, see [Operations and Troubleshooting](operations.md).
