# External Identity Providers

Use this guide when you want NiFi-Fabric to authenticate through a
customer-managed identity system instead of the standard single-user bootstrap
path.

NiFi-Fabric already supports:

- OIDC through `auth.mode=oidc`
- LDAP through `auth.mode=ldap`

This page is the operator-facing entry point for those advanced paths. It keeps
the core contract explicit and provider-owned:

- NiFi-Fabric consumes standard OIDC or LDAP inputs
- your identity provider remains the source of truth for users, passwords, and
  membership
- NiFi-Fabric does not provision realms, clients, users, or directory objects

## Choose Your Path

Use:

- OIDC when you want browser login through an external identity provider plus
  group-based authorization through `authz.mode=externalClaimGroups`
- LDAP when you want native directory-backed login and the current
  `authz.mode=ldapSync` path

Current recommended tracks:

- customer-managed production OIDC
  - best fit for standards-based IdPs such as Keycloak and similar OIDC
    providers
  - recommended when your identity team already owns realm, client, group, and
    user lifecycle
- dev bootstrap OIDC
  - convenience path for local, personal, and demo environments
  - uses a small Keycloak bootstrap example, not the enterprise ownership model
- production LDAP
  - recommended when a directory-backed LDAP or AD path is the required
    customer standard

See:

- [Authentication](../manage/authentication.md)
- [Advanced Install Paths](advanced.md)
- [Keycloak OIDC Production Setup](keycloak-oidc-production.md)
- [LDAP Production Setup](ldap-production.md)

## OIDC Setup Contract

For OIDC, NiFi-Fabric needs a small set of inputs to line up exactly with the
customer-managed identity-provider configuration.

### Discovery URL

Set `auth.oidc.discoveryUrl` to the real issuer discovery endpoint for the
realm or tenant that owns the NiFi client.

Example:

```yaml
auth:
  mode: oidc
  oidc:
    discoveryUrl: https://idp.example.com/realms/nifi/.well-known/openid-configuration
```

### Client ID and Client Secret

Set:

- `auth.oidc.clientId`
- `auth.oidc.clientSecret.existingSecret`
- `auth.oidc.clientSecret.key`

The referenced Kubernetes Secret should hold the confidential OIDC client
secret, not a user password or IdP admin credential.

### Identity and Group Claims

The key claims are:

- `auth.oidc.claims.identifyingUser`
- `auth.oidc.claims.groups`

Those must match what the IdP actually emits in tokens. For example:

```yaml
auth:
  oidc:
    claims:
      identifyingUser: email
      groups: groups
```

If the IdP emits group names differently than NiFi-Fabric expects, access will
look wrong even when authentication succeeds.

### Bootstrap Admin Identity or Group

NiFi still needs an explicit first-admin path.

For the normal OIDC path, the recommended shape is:

```yaml
authz:
  mode: externalClaimGroups
  bootstrap:
    initialAdminGroup: nifi-platform-admins
```

That group must already exist in the IdP before first login.

### Proxy Host and Redirect URI Alignment

The browser-facing NiFi host must line up across:

- the IdP client redirect URI
- the IdP client web origin settings when required
- `nifi.web.proxyHosts`

If the user browses to `https://nifi.example.com`, then that same public host
must be reflected in both the IdP client settings and the NiFi values.

### Private CA Trust

If the IdP uses a private CA, NiFi must trust that CA before OIDC discovery and
token validation can succeed.

Use the existing additional trust-bundle path:

```yaml
tls:
  additionalTrustBundle:
    enabled: true
    secretRef:
      name: idp-ca
      key: ca.crt
```

See [TLS and cert-manager](../manage/tls-and-cert-manager.md) for the trust
bundle options.

## Keycloak Example

Keycloak is the best concrete example here because the current repo already
includes Keycloak-oriented OIDC overlays and walkthroughs. The contract stays
provider-agnostic, but the example workflow below is the recommended customer
shape.

Before installing NiFi-Fabric, create or confirm:

1. a Keycloak realm for NiFi
2. a confidential OIDC client for NiFi
3. a groups mapper on that client
4. the Keycloak groups NiFi-Fabric will seed and bind
5. at least one real bootstrap admin user in the bootstrap admin group
6. `Secret/nifi-oidc` with the Keycloak client secret
7. NiFi values that match the realm URL, client ID, claim names, and public
   NiFi host

Recommended Keycloak group model:

- `nifi-platform-admins`
- `nifi-flow-operators`
- `nifi-flow-observers`

Recommended install command:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/values-prod-oidc.yaml
```

Use:

- [values-prod-oidc.yaml](../../examples/values-prod-oidc.yaml)
- [Keycloak OIDC Production Setup](keycloak-oidc-production.md)

For local or demo-only bootstrap, use:

- [values-dev-keycloak-bootstrap.yaml](../../examples/values-dev-keycloak-bootstrap.yaml)
- [keycloak-dev-bootstrap-realm.json](../../examples/keycloak-dev-bootstrap-realm.json)

That dev bootstrap path is convenience-only and should not be presented as the
enterprise ownership model.

## OIDC Authorization Behavior

For `authz.mode=externalClaimGroups`, NiFi-Fabric owns:

- the NiFi application-group catalog
- bundle bindings
- the bootstrap admin group selection

The external IdP owns:

- users
- passwords
- membership in existing external groups
- realm and client configuration

This leads to an important operational split.

### Changes That Do Not Require a NiFi Rollout

- adding a new user in the IdP
- changing a user password in the IdP
- adding a user to an already-seeded external group

### Changes That Usually Require Only Re-Login

- changing group membership for a user who already has an active NiFi session

Treat that as a token-refresh or re-login event, not a NiFi restart event.

### Changes That Require Values or Authz Updates and Rollout

- adding a brand-new group name that NiFi-Fabric has not seeded yet
- changing group-to-bundle mappings
- changing the bootstrap admin group
- changing discovery URL, client ID, client secret reference, claim names, or
  proxy-host alignment

## LDAP Contract

LDAP remains a first-class path, but it is intentionally narrower than OIDC.

Today the supported LDAP model is:

- `auth.mode=ldap`
- `authz.mode=ldapSync`
- explicit bootstrap admin identity or bootstrap admin group
- customer-managed directory connectivity, bind credentials, and search settings

Use LDAP when you want:

- directory-backed login
- directory-backed user and group lookups
- explicit first-admin bootstrap without the richer OIDC group-to-bundle model

Use:

- [ldap-values.yaml](../../examples/ldap-values.yaml)
- [LDAP Production Setup](ldap-production.md)

## Common OIDC Mistakes

If OIDC authentication works but access does not, check:

- the actual groups claim name in the token
- whether the IdP emits simple group names or full paths
- whether the NiFi application-group values use the same shape
- whether the bootstrap admin group exists before first login
- whether `nifi.web.proxyHosts` matches the real browser URL and client redirect
  URI
- whether private CA trust is configured when the IdP is not publicly trusted

If browser login does not start cleanly, check:

- discovery URL correctness
- OIDC client redirect URI and web origin settings
- `Secret/nifi-oidc` name and key
- any ingress, Route, or reverse-proxy host mismatch

## Boundary

This guidance improves the install story, but it does not widen the product
scope.

NiFi-Fabric does not become:

- a Keycloak operator
- a realm or client provisioning system
- a user or password sync engine
- a provider-specific API surface in the chart

The product contract stays boring:

- explicit `auth.*` values
- explicit `authz.*` values
- explicit Secret ownership
- explicit trust and proxy-host inputs
