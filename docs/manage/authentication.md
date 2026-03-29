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

Optional external tooling can generate the `externalClaimGroups` overlay from Keycloak group metadata plus a small mapping file. This remains a Git-rendered input path and does not change NiFi's underlying authorization contract.

Recommended OIDC install tracks:

- dev bootstrap:
  - convenience workflow for local, personal, and demo environments
  - bootstrap or import a small Keycloak realm plus groups, client, and optional dev admin user
  - install NiFi with the matching bootstrap admin group and authz overlay
  - use [values-dev-keycloak-bootstrap.yaml](../../examples/values-dev-keycloak-bootstrap.yaml)
- production OIDC:
  - recommended customer workflow
  - customer-owned realm, groups, users, and client setup first
  - install NiFi only after the identity model and bootstrap admin group are ready
  - use [values-prod-oidc.yaml](../../examples/values-prod-oidc.yaml)

The dev bootstrap path is convenience-only. It is not the recommended enterprise ownership model.

OpenShift example path:

- install through `charts/nifi-platform`
- external access through an OpenShift `Route`
- explicit Route host rendered into `nifi.web.proxy.host`
- `authz.mode=externalClaimGroups`
- external claim groups bound to the named NiFi bundles `admin`, `viewer`, `editor`, and `flowVersionManager`

The OpenShift example overlay is [oidc-managed-values.yaml](../../examples/openshift/oidc-managed-values.yaml).

See:

- [External Identity Providers](../install/external-identity-providers.md)
- [Advanced Install Paths](../install/advanced.md)
- [Integrated OIDC Install Contract](../install/integrated-oidc.md)
- [Keycloak OIDC Production Setup](../install/keycloak-oidc-production.md)
- [TLS and cert-manager](tls-and-cert-manager.md) when the IdP needs custom CA trust
- [Authz Tooling Pointer](../../tools/nifi-fabric-authz/README.md)
- [Dev Keycloak Bootstrap Realm Example](../../examples/keycloak-dev-bootstrap-realm.json)

### Post-Deploy OIDC Changes

For `authz.mode=externalClaimGroups`, NiFi-Fabric seeds the matching NiFi groups and bundle bindings, but Keycloak remains the source of user membership.

Operational behavior:

- adding a new user to an already-seeded Keycloak group does not require a NiFi restart
- that new user can log in after the Keycloak change is live and receive the expected access
- changing group membership for a user who is already logged in should be treated as a re-login or token-refresh event, not a NiFi restart event
- adding a brand-new Keycloak group name that is not already present in the NiFi authz overlay does require an updated overlay and rollout

In short:

- users and memberships can change at runtime
- group catalog and bundle bindings remain Git-rendered install inputs

Common troubleshooting:

- if users authenticate but the expected group access never appears, check whether Keycloak emits group names or full paths and make sure the generated overlay uses the same shape
- if a user is added to a brand-new Keycloak group, regenerate and redeploy the NiFi authz overlay because NiFi-Fabric must seed that group explicitly

Identity ownership reminder:

- Keycloak remains the source of truth for users, passwords, and group membership
- NiFi-Fabric does not reset Keycloak passwords or reconcile Keycloak user state
- any repeated user or password overwrite behavior comes from the Keycloak bootstrap mechanism, not from NiFi-Fabric

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

The OpenShift example overlay is [ldap-managed-values.yaml](../../examples/openshift/ldap-managed-values.yaml).

Current limitation:

- current OpenShift coverage here includes the bootstrap-admin identity path for LDAP
- current OpenShift coverage here does not include LDAP group-bootstrap or named bundle mapping in this path
- the baseline LDAP example uses `authz.bootstrap.initialAdminIdentity`
- LDAP initial-admin-group bootstrap is supported on newer NiFi images and should be treated as an advanced path

See:

- [External Identity Providers](../install/external-identity-providers.md)
- [Advanced Install Paths](../install/advanced.md)
- [LDAP Production Setup](../install/ldap-production.md)

## Authorization Model

NiFi-Fabric supports:

- file-managed authorization for the standard single-user path
- external-claim-group authorization for the OIDC path
- LDAP-sync authorization for the LDAP path

The project also includes named authorization bundles for common access patterns such as viewer, editor, flow-version-manager, and admin.

Current coverage is:

- OIDC: external groups map into named NiFi bundles
- LDAP: current coverage includes native login, explicit bootstrap-admin identity on the default image, and initial-admin-group bootstrap on newer NiFi images

### Post-Deploy LDAP Changes

For the current `ldap + ldapSync` path:

- users and passwords remain directory-owned
- user and group changes should follow LDAP sync or the next relevant auth lookup path
- changing directory membership for existing groups should not require a NiFi restart
- changing bootstrap admin intent or NiFi-side LDAP settings still requires a values change and rollout
- LDAP group bootstrap on newer NiFi images depends on the directory-backed user-group provider, so keep the user-group-provider user search filter aligned with directory lookup rather than the login filter

## Next Steps

- [Install with Helm](../install/helm.md)
- [Advanced Install Paths](../install/advanced.md)
- [External Identity Providers](../install/external-identity-providers.md)
- [TLS and cert-manager](tls-and-cert-manager.md)
