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

## Standard Install Path

Install cert-manager first, create or choose the `Issuer` or `ClusterIssuer`, then install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-quickstart-values.yaml
```

This standard path does not require pre-created bootstrap auth or TLS Secrets.

## Read Next

- [Features](features.md)
- [Install with Helm](install/helm.md)
- [First Access and Day-1 Checks](first-day.md)
- [Advanced Install Paths](install/advanced.md)
- [Compatibility](compatibility.md)
- [Operations and Troubleshooting](operations.md)
