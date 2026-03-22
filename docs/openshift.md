# OpenShift

OpenShift is supported for NiFi-Fabric.

The recommended OpenShift path is the same standard customer install used on other Kubernetes platforms:

- `charts/nifi-platform`
- managed mode
- cert-manager-first TLS
- internal `ClusterIP` access by default
- OpenShift `Route` when you need external HTTPS access

## Recommended OpenShift Install

Install cert-manager first, then use the standard quickstart profile with the OpenShift overlay:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  --set global.nifiFabric.installProfile=quickstart-cert-manager \
  --set nifi.tls.certManager.issuerRef.name=nifi-ca \
  -f examples/openshift/managed-values.yaml
```

This is the main OpenShift starting point for customer installs.

## External Access With Route

If you want external HTTPS access, add the Route overlay:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  --set global.nifiFabric.installProfile=quickstart-cert-manager \
  --set nifi.tls.certManager.issuerRef.name=nifi-ca \
  -f examples/openshift/managed-values.yaml \
  -f examples/openshift/route-proxy-host-values.yaml
```

Before using that overlay, change the example hostname in `examples/openshift/route-proxy-host-values.yaml` to the Route host you want to expose.

If you prefer to bring your own auth or TLS Secrets instead of using the quickstart path, use [Advanced Install Paths](install/advanced.md).

## What You Need On OpenShift

Prepare:

- an OpenShift cluster
- a working `StorageClass` for NiFi PVCs
- a controller image the cluster can pull
- a NiFi image the cluster can pull
- cert-manager and the `Issuer` or `ClusterIssuer` you want to use for the standard TLS path

If your cluster does not have a suitable default `StorageClass`, set `nifi.persistence.storageClassName` in your values before installing.

## Notes For OpenShift

- Keep the standard `charts/nifi-platform` install path.
- Start with internal service access unless you need a public endpoint.
- Use an OpenShift `Route` when you want the native external access model.
- Keep NiFi TLS enabled end to end.

## Read Next

- [Install with Helm](install/helm.md)
- [First Access and Day-1 Checks](first-day.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Authentication](manage/authentication.md)
- [Operations and Troubleshooting](operations.md)
