# Verification and Support Levels

This page explains how NiFi-Fabric support claims are backed by repository verification.

## Verification Layers

NiFi-Fabric uses three main layers:

- unit and envtest coverage for controller behavior
- Helm lint and render coverage for chart behavior
- focused runtime verification for the standard product paths

## What This Means for Customers

The project does not treat “renders successfully” and “runtime-proven” as the same thing.

In practice:

- the standard install path has the strongest verification focus
- advanced paths are supported, but some are documented more conservatively
- feature-specific support detail lives on the relevant install, manage, and compatibility pages

## Standard Verified Areas

Repository verification is centered on:

- the standard managed install through `charts/nifi-platform`
- cert-manager-first TLS
- core lifecycle behavior such as rollout, TLS restart handling, hibernation, and restore
- primary authentication and metrics paths
- controller-owned autoscaling

Current focused OpenShift runtime proofs include:

- managed install through `charts/nifi-platform`
- cert-manager-first TLS issuance for the standard managed install shape
- native passthrough `Route` rendering, admission, service mapping, and external access
- recommended `nativeApi` metrics rendering through the chart-managed `Service` and `ServiceMonitor` objects
- live secured `nativeApi` metrics scrape coverage for `/nifi-api/flow/metrics/prometheus`
- OIDC login through the Route with named `admin`, `viewer`, `editor`, and `flowVersionManager` bundle checks
- LDAP login through the Route on the documented bootstrap-admin identity path

## How to Read Support Claims

Use these pages together:

- [Compatibility](compatibility.md) for supported versions and environments
- [Install with Helm](install/helm.md) for the standard install path
- [Advanced Install Paths](install/advanced.md) for non-standard installs
- [Experimental Features](experimental-features.md) for intentionally non-GA areas

When a feature needs more nuance, the detailed support position belongs on that feature page rather than being repeated everywhere.

## Internal Verification Detail

This repository also contains focused runtime workflows, proof commands, and narrower validation paths used by maintainers.

Recent focused OpenShift verification uses:

- `go test ./...`
- `helm lint charts/nifi`
- `helm lint charts/nifi-platform`
- `helm template nifi-cert charts/nifi-platform -f examples/platform-managed-cert-manager-values.yaml -f examples/openshift/managed-values.yaml`
- `helm template nifi-cert-metrics charts/nifi-platform -f examples/platform-managed-cert-manager-values.yaml -f examples/platform-managed-metrics-native-values.yaml -f examples/openshift/managed-values.yaml`
- `helm template nifi charts/nifi-platform -f examples/platform-managed-values.yaml -f examples/openshift/managed-values.yaml -f examples/openshift/oidc-managed-values.yaml -f examples/openshift/route-proxy-host-values.yaml`
- `helm template nifi charts/nifi-platform -f examples/platform-managed-values.yaml -f examples/openshift/managed-values.yaml -f examples/openshift/ldap-managed-values.yaml -f examples/openshift/route-proxy-host-values.yaml`
- `helm upgrade --install cert-manager jetstack/cert-manager --namespace cert-manager --create-namespace --version v1.19.2 --set crds.enabled=true --wait --timeout 10m`
- bootstrap `Issuer/nifi-selfsigned-bootstrap`, `Certificate/nifi-root-ca`, and `ClusterIssuer/nifi-ca`
- `helm upgrade --install nifi-cert-native charts/nifi-platform -n nifi-cert-native -f examples/platform-managed-cert-manager-values.yaml -f examples/openshift/managed-values.yaml --set controller.namespace.name=nifi-system --set controller.namespace.create=false --set nifi.replicaCount=1`
- `bash hack/bootstrap-metrics-machine-auth.sh --namespace nifi-cert-native --auth-mode authorizationHeader --source-auth-secret nifi-auth --statefulset nifi-cert-native --mint-token`
- `helm upgrade --install nifi-cert-native charts/nifi-platform -n nifi-cert-native -f examples/platform-managed-cert-manager-values.yaml -f examples/platform-managed-metrics-native-values.yaml -f examples/openshift/managed-values.yaml --set controller.namespace.name=nifi-system --set controller.namespace.create=false --set nifi.replicaCount=1`
- `make openshift-platform-managed-oidc-proof`
- `make openshift-platform-managed-ldap-proof`

Those details are useful for engineering work, but they are not the main customer entrypoint. Customer-facing docs should prefer:

- what is supported
- what the standard path is
- where the advanced path begins

over raw proof-command inventories.
