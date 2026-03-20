# Authentication

NiFi-Fabric supports three authentication modes:

- `singleUser`
- `oidc`
- `ldap`

Use app chart values under `auth.*` and `authz.*`, or platform chart values under `nifi.auth.*` and `nifi.authz.*`.

## Standard Path

The standard cert-manager-first install uses `singleUser` authentication.

That path bootstraps `Secret/nifi-auth` automatically in the release namespace, so you do not create it yourself before install.

## OIDC

Use OIDC when you want:

- browser login through an identity provider
- external groups mapped into NiFi authorization
- explicit bootstrap admin identity or group

OIDC belongs to the advanced install path. It does not depend on the standard single-user bootstrap Secret.

See:

- [Advanced Install Paths](../install/advanced.md)
- [TLS and cert-manager](tls-and-cert-manager.md) when the IdP needs custom CA trust

## LDAP

Use LDAP when you want:

- directory-backed login
- LDAP-backed group or user lookups
- explicit bootstrap admin identity or group

LDAP also belongs to the advanced install path. It does not depend on the standard single-user bootstrap Secret.

See:

- [Advanced Install Paths](../install/advanced.md)

## Authorization Model

NiFi-Fabric supports:

- file-managed authorization for the standard single-user path
- external-claim-group authorization for the bounded OIDC path
- LDAP-sync authorization for the LDAP path

The project also includes named authorization bundles for common access patterns such as viewer, editor, and admin.

## Next Steps

- [Install with Helm](../install/helm.md)
- [Advanced Install Paths](../install/advanced.md)
- [TLS and cert-manager](tls-and-cert-manager.md)
