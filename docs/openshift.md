# OpenShift Baseline Guide

This guide describes the current OpenShift starting point for NiFi-Fabric.

OpenShift is a supported secondary target environment. The product architecture does not change on OpenShift:

- `charts/nifi-platform` remains the standard install path
- the controller remains the lifecycle owner in managed mode
- the starting point stays internal first

## Recommended Starting Point

Start with:

- the managed install through `charts/nifi-platform`
- internal `ClusterIP` access first
- the OpenShift overlay for the standard managed path

## Baseline Install Shape

The baseline composition is:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`

Use this path when you want to adapt NiFi-Fabric to OpenShift security-context and storage expectations without changing the product model.

## What to Prepare

Before installing, make sure you have:

- an OpenShift cluster with PVC support for NiFi
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- the required release-namespace Secrets for the path you choose
- an appropriate storage class

## Current Position

The documented OpenShift baseline is intentionally conservative:

- internal service access first
- external Route exposure later if needed
- auth and TLS choices should follow the same standard-versus-advanced install split used elsewhere in the docs

## Next Steps

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md)
- [Authentication](manage/authentication.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
