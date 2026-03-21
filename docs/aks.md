# AKS

AKS is the primary supported target environment for NiFi-Fabric.

The recommended AKS deployment model is the standard managed install through `charts/nifi-platform` with cert-manager-first TLS.

## Recommended AKS Shape

Use this shape on AKS:

- `charts/nifi-platform`
- managed mode
- cert-manager-first TLS
- PVC-backed NiFi repositories
- internal `ClusterIP` service first, then add ingress or a load balancer only when your deployment needs it

This is the main product path for AKS support and documentation.

## What You Need On AKS

Prepare:

- an AKS cluster with a suitable PVC-capable `StorageClass`
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- cert-manager and the referenced `Issuer` or `ClusterIssuer`

If you use Azure Container Registry for the controller image, make sure the AKS kubelet identity can pull from that registry before the first Helm install.

## Install Starting Point

Start with:

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md) when you need explicit inputs
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Operations and Troubleshooting](operations.md)

## Support Position

NiFi-Fabric works on AKS, and AKS is the primary target environment for the project.

The recommended customer path on AKS is the standard managed install with cert-manager. Advanced shapes remain available, but support and documentation center on that managed AKS path first.
