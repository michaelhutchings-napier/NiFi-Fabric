# TLS and cert-manager

NiFi-Fabric is TLS-first.

## Standard Path

The standard customer path is cert-manager-first:

- install cert-manager separately
- create or choose the `Issuer` or `ClusterIssuer`
- install NiFi-Fabric through `charts/nifi-platform`

In this path:

- the install bootstraps the supporting auth and parameter Secrets it needs
- Helm renders the `Certificate`
- cert-manager creates the workload TLS Secret

## Advanced Paths

Use the advanced install path when you want explicit ownership of TLS-related Secrets.

### External TLS Secret

Use this when you want to provide the workload TLS Secret yourself.

### Explicit Cert-Manager Inputs

Use this when you want cert-manager to own the workload certificate, but you still want to provide the supporting Secrets such as `nifi-auth` or `nifi-tls-params` yourself.

## trust-manager

trust-manager is optional.

Use it when you want to distribute a shared CA bundle for:

- metrics scraping
- outbound trust to systems such as OIDC, LDAP, or registry endpoints

trust-manager does not replace cert-manager, and it does not move TLS orchestration into the controller.

## Next Steps

- [Install with Helm](../install/helm.md)
- [Advanced Install Paths](../install/advanced.md)
- [Authentication](authentication.md)
