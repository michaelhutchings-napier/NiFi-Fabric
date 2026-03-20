# AKS Readiness Guide

This guide is the starting point for teams planning to run NiFi-Fabric on AKS.

AKS is a primary target environment, but you should still validate the standard install path in your own cluster and storage setup.

## Recommended Starting Point

Start with:

- the standard managed install through `charts/nifi-platform`
- cert-manager-first TLS
- internal cluster access first, before adding ingress or external exposure

Use:

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md) when you need explicit inputs

## What to Prepare

Before evaluating on AKS, make sure you have:

- an AKS cluster with PVC support suitable for NiFi
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- cert-manager and an issuer if you are using the standard path
- a storage class appropriate for NiFi repository volumes

## What to Validate in Your Environment

On AKS, validate:

- storage performance and PVC behavior
- image pull strategy
- internal service reachability
- ingress or load balancer exposure if needed
- cert-manager behavior in your cluster

## Next Steps

- [Install with Helm](install/helm.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Operations and Troubleshooting](operations.md)
