# OpenShift Baseline Guide

This guide describes the current OpenShift starting point for NiFi-Fabric.

OpenShift is a supported secondary target environment. The product architecture does not change on OpenShift:

- `charts/nifi-platform` remains the standard install path
- the controller remains the lifecycle owner in managed mode
- NiFi keeps ownership of its own TLS material
- OpenShift Route is the native external access surface when external access is needed

## Recommended Starting Point

Start with:

- the managed install through `charts/nifi-platform`
- internal `ClusterIP` access first
- the OpenShift overlay for the standard managed path
- the separate Route overlay only when you need external HTTPS access

## Baseline Install Shape

The baseline composition is:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`

Use this path when you want to adapt NiFi-Fabric to OpenShift security-context and storage expectations without changing the product model.

## Supported Route Shape

The bounded supported OpenShift external-access shape is:

- OpenShift `Route` as the only external access surface
- `passthrough` TLS termination as the primary runtime-proven shape
- explicit `openshift.route.host`
- matching `web.proxyHosts` entry for that same public host
- NiFi TLS still terminated by NiFi, not by the router

Compose the Route overlay with the managed OpenShift path:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`
- `examples/openshift/route-proxy-host-values.yaml`

This keeps the product boundary narrow:

- no new CRDs
- no generic ingress abstraction beyond the existing chart support
- no change to controller lifecycle scope
- no redesign of NiFi TLS ownership

## Explicit Assumptions

The supported Route model assumes:

- the public Route hostname is explicit at install time
- `web.proxyHosts` includes that Route hostname, optionally as `host:443`
- the NiFi certificate presented through the passthrough Route is valid for the public Route hostname
- browser and API traffic use the same HTTPS host

What this does not promise:

- path-based passthrough behavior
- router-managed TLS termination for the primary supported proof shape
- automatic discovery of a generated Route hostname and automatic back-propagation into NiFi proxy settings

## Runtime-Proven Route Behavior

The repository now includes a focused OpenShift Route proof path on the standard managed install flow:

```bash
make openshift-platform-managed-route-proof
```

That proof path verifies:

- the Route renders and applies through `charts/nifi-platform`
- the Route is admitted by the OpenShift router
- the Route maps to the expected NiFi `Service` backend and named `https` port
- NiFi renders the expected `nifi.web.proxy.host` value
- secure browser access works through `https://<route-host>/nifi/`
- secure authenticated API access works through `https://<route-host>/nifi-api/...`

If `ROUTE_HOST` is not set, the proof helper derives one from the cluster apps domain. You can also set it explicitly:

```bash
ROUTE_HOST=nifi.apps.example.com make openshift-platform-managed-route-proof
```

## What to Prepare

Before installing, make sure you have:

- an OpenShift cluster with PVC support for NiFi
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- the required release-namespace Secrets for the path you choose
- an appropriate storage class
- for passthrough Route proof, NiFi TLS material that is trusted by clients and valid for the chosen Route host

## Diagnostics

The focused Route proof collects and surfaces:

- `Route` status and full YAML
- router admission state
- Route-to-Service backend mapping
- NiFi `nifi.web.https.host`, `nifi.web.https.port`, and `nifi.web.proxy.host` settings
- release, controller, workload, and event diagnostics on failure

Useful commands:

```bash
helm -n nifi status nifi
oc -n nifi get route nifi -o yaml
oc -n nifi describe route nifi
oc -n nifi get svc nifi -o yaml
oc -n nifi get endpointslice -l kubernetes.io/service-name=nifi
oc -n nifi exec nifi-0 -c nifi -- grep '^nifi\.web\.proxy\.host=' /opt/nifi/nifi-current/conf/nifi.properties
```

## Next Steps

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md)
- [Authentication](manage/authentication.md)
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
