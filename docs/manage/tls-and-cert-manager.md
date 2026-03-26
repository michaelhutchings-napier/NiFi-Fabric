# TLS and cert-manager

NiFi-Fabric is TLS-first.

The workload TLS material consumed by NiFi is always mounted from a normal Kubernetes `Secret` volume.

The main question is who writes that Secret and where the related password material lives:

- `tls.mode=externalSecret`: you create the workload TLS Secret yourself
- `tls.mode=certManager`: Helm renders a cert-manager `Certificate`, cert-manager writes the workload TLS Secret, and NiFi still needs a PKCS12 password plus a sensitive properties key from values or a supporting Secret

## Quick Comparison

| Path | `nifi-tls` | `nifi-auth` | `nifi-tls-params` | Best for |
| --- | --- | --- | --- | --- |
| Standard quickstart cert-manager | cert-manager creates it | chart creates it | chart creates it | first install and normal customer path |
| Explicit cert-manager | cert-manager creates it | you create it | you create it | GitOps and explicit Secret ownership |
| External Secret / BYO TLS | you create it | you create it | not used | pre-existing enterprise TLS bundles |

If you are asking "do I need to create `nifi-tls` first?", the answer is:

- no for the standard quickstart cert-manager path
- no for the explicit cert-manager path
- yes only for `tls.mode=externalSecret`

## Standard Path

The standard customer path is cert-manager-first:

- install cert-manager separately
- create or choose the `Issuer` or `ClusterIssuer`
- install NiFi-Fabric through `charts/nifi-platform`

In this path:

- the install bootstraps the supporting auth and parameter Secrets it needs
- Helm renders the `Certificate`
- cert-manager creates the workload TLS Secret

The standard managed quickstart does not require you to pre-create the bootstrap auth or TLS support Secrets.

By default:

- quickstart writes `Secret/nifi-auth`
- quickstart writes `Secret/nifi-tls-params`
- cert-manager writes `Secret/nifi-tls`

The platform chart keeps those generated Secrets stable by name so the later handoff to the explicit cert-manager path can stay in place.

## Advanced Paths

Use the advanced install path when you want explicit ownership of TLS-related Secrets.

### External TLS Secret

Use this when you want to provide the workload TLS Secret yourself.

This is the only path where you create `Secret/nifi-tls` yourself before install.

In that path, the app chart expects one Secret, `nifi-tls` by default, with these keys:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- `keystorePassword`
- `truststorePassword`
- `sensitivePropsKey`

That Secret is mounted directly into the NiFi pod at `/opt/nifi/tls` by default.

See:

- [tls-external-secret.yaml](../../examples/secret-contracts/tls-external-secret.yaml)
- [auth-single-user-secret.yaml](../../examples/secret-contracts/auth-single-user-secret.yaml)

### Explicit Cert-Manager Inputs

Use this when you want cert-manager to own the workload certificate, but you still want to provide the supporting Secrets such as `nifi-auth` or `nifi-tls-params` yourself.

In this path, you still do not create `Secret/nifi-tls` yourself. cert-manager writes it after the chart renders the `Certificate`.

In that path:

- Helm renders a cert-manager `Certificate`
- cert-manager writes `Secret/nifi-tls`
- you provide `Secret/nifi-tls-params`
- you provide `Secret/nifi-auth` when using the standard single-user auth path

`nifi-tls-params` carries the password and sensitive-properties inputs that are not written into the cert-manager target Secret:

- `pkcs12Password`
- `sensitivePropsKey`

See:

- [tls-cert-manager-params-secret.yaml](../../examples/secret-contracts/tls-cert-manager-params-secret.yaml)
- [auth-single-user-secret.yaml](../../examples/secret-contracts/auth-single-user-secret.yaml)

## Secret Contracts

### Single-User Auth Secret

`auth.mode=singleUser` uses `Secret/nifi-auth` by default with:

- `username`
- `password`

### External TLS Secret Contract

`tls.mode=externalSecret` uses `Secret/nifi-tls` by default with:

- `ca.crt`: PEM CA certificate used by NiFi and related integrations
- `keystore.p12`: PKCS12 keystore containing the workload certificate and private key
- `truststore.p12`: PKCS12 truststore consumed by NiFi
- `keystorePassword`: password for `keystore.p12`
- `truststorePassword`: password for `truststore.p12`
- `sensitivePropsKey`: `nifi.sensitive.props.key`

### Cert-Manager Contract

`tls.mode=certManager` splits ownership:

- `Certificate/<release>` is rendered by Helm
- `Secret/nifi-tls` is written by cert-manager
- `Secret/nifi-tls-params` is written either by the platform quickstart or by you on the explicit path

The chart and e2e coverage expect the cert-manager target TLS Secret to contain:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`

The chart does not expect `keystorePassword`, `truststorePassword`, or `sensitivePropsKey` inside the cert-manager target Secret.

Instead:

- `pkcs12Password` comes from `nifi-tls-params` or an inline value
- `sensitivePropsKey` comes from `nifi-tls-params`, another referenced Secret, or an inline value

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
