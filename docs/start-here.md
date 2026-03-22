# Start Here

NiFi-Fabric is a Kubernetes platform for Apache NiFi 2.x.

It gives you a standard Helm install path, a thin operational controller for lifecycle and safety work, and a simpler product model than a broad NiFi-specific operator stack.

## What You Install

NiFi-Fabric has two chart surfaces:

- `charts/nifi-platform` is the standard customer-facing install path
- `charts/nifi` is the reusable standalone app chart for lower-level workflows

For most teams, `charts/nifi-platform` is the right starting point.

## Why Teams Use It

- one clear Helm install path
- secure-by-default, cert-manager-first installation
- safe rollout, TLS restart handling, hibernation, restore, and autoscaling
- support for single-user, OIDC, and LDAP authentication models
- clear boundaries between Helm, Kubernetes, NiFi, and the controller

## Choose Your Path

### Standard Production Install

Use `charts/nifi-platform` with the standard cert-manager-first install flow.

This is the recommended customer path when cert-manager and the target `Issuer` or `ClusterIssuer` already exist in the cluster.

Start with [Install with Helm](install/helm.md).

### Quick Evaluation Install

Use the standard managed quickstart example when you want the fastest supported first install on the standard product path:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  --set global.nifiFabric.installProfile=quickstart-cert-manager \
  --set nifi.tls.certManager.issuerRef.name=nifi-ca
```

This path bootstraps the initial single-user login and TLS inputs, while cert-manager creates the final workload TLS Secret.

The quickstart path is valid for evaluation and low-friction first installs, but it is not a substitute for explicit enterprise auth planning.

After install, continue with [First Access and Day-1 Checks](first-day.md).

### Advanced Explicit Auth And TLS Install

Use the advanced path when you want explicit ownership of auth and TLS inputs from the start, including OIDC, LDAP, or explicit Secret-managed TLS inputs.

Start with [Advanced Install Paths](install/advanced.md).

## Read Next

- [Features](features.md)
- [Install with Helm](install/helm.md)
- [First Access and Day-1 Checks](first-day.md)
- [Advanced Install Paths](install/advanced.md)
- [Compatibility](compatibility.md)
- [Operations and Troubleshooting](operations.md)
