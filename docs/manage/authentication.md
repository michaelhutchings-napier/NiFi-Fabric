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

OpenShift example path:

- install through `charts/nifi-platform`
- external access through an OpenShift `Route`
- explicit Route host rendered into `nifi.web.proxy.host`
- `authz.mode=externalClaimGroups`
- external claim groups bound to the named NiFi bundles `admin`, `viewer`, `editor`, and `flowVersionManager`

The OpenShift example overlay is [oidc-managed-values.yaml](/home/michael/Work/nifi2-platform/examples/openshift/oidc-managed-values.yaml).

See:

- [Advanced Install Paths](../install/advanced.md)
- [TLS and cert-manager](tls-and-cert-manager.md) when the IdP needs custom CA trust

## LDAP

Use LDAP when you want:

- directory-backed login
- LDAP-backed group or user lookups
- explicit bootstrap admin identity or group

LDAP also belongs to the advanced install path. It does not depend on the standard single-user bootstrap Secret.

OpenShift example path:

- install through `charts/nifi-platform`
- external access through an OpenShift `Route`
- `authz.mode=ldapSync`
- explicit bootstrap admin identity

The OpenShift example overlay is [ldap-managed-values.yaml](/home/michael/Work/nifi2-platform/examples/openshift/ldap-managed-values.yaml).

Current limitation:

- current OpenShift coverage here includes the bootstrap-admin identity path for LDAP
- current OpenShift coverage here does not include LDAP group-bootstrap or named bundle mapping in this path

See:

- [Advanced Install Paths](../install/advanced.md)

## Authorization Model

NiFi-Fabric supports:

- file-managed authorization for the standard single-user path
- external-claim-group authorization for the OIDC path
- LDAP-sync authorization for the LDAP path

The project also includes named authorization bundles for common access patterns such as viewer, editor, flow-version-manager, and admin.

Current coverage is:

- OIDC: external groups map into named NiFi bundles
- LDAP: current coverage includes login plus the explicit bootstrap-admin identity path

## Next Steps

- [Install with Helm](../install/helm.md)
- [Advanced Install Paths](../install/advanced.md)
- [TLS and cert-manager](tls-and-cert-manager.md)
