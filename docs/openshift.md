# OpenShift Baseline Guide

OpenShift is a supported secondary target environment for NiFi-Fabric.

The recommended OpenShift deployment model keeps the same product shape as standard Kubernetes:

- `charts/nifi-platform` remains the standard install path
- the controller remains the lifecycle owner in managed mode
- NiFi keeps ownership of its own TLS material
- OpenShift `Route` is the native external access surface when external access is needed

## Recommended Starting Point

Start with:

- the managed install through `charts/nifi-platform`
- internal `ClusterIP` access first
- the OpenShift managed overlay for the standard path
- the separate Route overlay only when you need external HTTPS access

## Supported OpenShift Shape

The supported baseline composition is:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`

For the native OpenShift external-access model, add:

- `examples/openshift/route-proxy-host-values.yaml`

This keeps the product boundary narrow and predictable:

- no new CRDs
- no separate OpenShift-specific control plane
- no change to controller lifecycle scope
- no change to NiFi TLS ownership

## Supported Route Shape

The supported OpenShift external-access shape is:

- OpenShift `Route` as the external access surface
- `passthrough` TLS termination
- explicit `openshift.route.host`
- matching `web.proxyHosts` entry for that same public host
- NiFi TLS still terminated by NiFi, not by the router

## What to Prepare

Before installing, make sure you have:

- an OpenShift cluster with PVC support for NiFi
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- the required release-namespace Secrets for the path you choose
- cert-manager plus the referenced issuer or `ClusterIssuer` if you want the standard managed TLS path
- an appropriate storage class

## Support Position

OpenShift is supported for the documented managed install path.

The customer-facing OpenShift shape is:

- managed install through `charts/nifi-platform`
- the OpenShift managed overlay
- the native passthrough `Route` model when external HTTPS access is required
- cert-manager-first TLS when you want the standard managed TLS path

## Next Steps

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md)
- [Authentication](manage/authentication.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
