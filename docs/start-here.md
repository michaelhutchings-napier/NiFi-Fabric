# Start Here

NiFi-Fabric is for teams that want to run Apache NiFi 2.x on Kubernetes with a clear product install path, a thin operational controller, and chart-first ownership of the standard Kubernetes resources around NiFi.

## What It Is

NiFi-Fabric packages two install surfaces around the same product model:

- `charts/nifi-platform` is the standard customer-facing chart
- `charts/nifi` is the reusable standalone app chart

The platform chart installs the controller, the app chart, and the `NiFiCluster` custom resource in one Helm release.

## Who It Is For

NiFi-Fabric is aimed at platform teams and application teams that want:

- a standard Helm install for NiFi on Kubernetes
- safe lifecycle operations for rollout, TLS handling, hibernation, and restore
- controller-owned autoscaling rather than direct `StatefulSet` scaling by a second autoscaler
- first-class managed authentication options
- small named policy bundles for common viewer, editor, version-manager, and admin roles
- a simpler product surface than a large NiFi-specific operator stack

## Why It Exists

NiFi is stateful and lifecycle-sensitive. Simple `StatefulSet` management is not enough when you need:

- safe restart and rollout sequencing
- TLS drift policy
- hibernation and restore
- controller-owned autoscaling decisions
- explicit ownership boundaries between Helm, Kubernetes, NiFi, and the controller

NiFi-Fabric keeps Helm in charge of ordinary Kubernetes resources and keeps the controller focused on the lifecycle work that needs runtime coordination.

## Main Features

- one-release platform install with `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model
- safe rollout, hibernation, and restore
- controller-owned autoscaling
- optional KEDA integration
- supported cert-manager integration
- optional trust-manager CA bundle distribution
- OIDC and LDAP support for managed deployments
- Git-based Flow Registry Client catalog support
- first-class observability and metrics subsystem

## Standard Install Path

The standard customer path is:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

You provide the Secrets and any optional cluster prerequisites required by your chosen install variant. See [Install with Helm](install/helm.md) for the mode-specific prerequisites.

If you need a manifest-based secondary path, NiFi-Fabric also ships a generated install bundle rendered from the same platform chart. See [Advanced Install Paths](install/advanced.md). Helm remains the primary recommendation.

## Read Next

- [Features](features.md)
- [Install with Helm](install/helm.md)
- [Compatibility](compatibility.md)
- [NiFiCluster Reference](reference/nificluster.md)
