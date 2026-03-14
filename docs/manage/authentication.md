# Authentication

NiFi-Fabric supports three customer-facing authentication patterns.

## Available Modes

- `singleUser`
- `oidc`
- `ldap`

These are configured in the app chart under:

- `auth.mode`
- `auth.*`
- `authz.*`

In the platform chart, use the same values under `nifi.auth.*` and `nifi.authz.*`.

## Single-User

Use single-user mode for simple environments, early evaluation, and break-glass recovery.

Required Secret:

- `Secret/nifi-auth`

## OIDC

OIDC is a first-class managed authentication option.

Use OIDC when you want:

- enterprise identity provider login
- browser-based access with external identity
- group-based authorization with NiFi-managed group seeding

Current proof boundary:

- managed OIDC wiring, discovery configuration, and seeded group definitions are render-validated
- the multi-group `authorizations.xml` seed path now renders in a NiFi 2-compatible order and no longer crashes the cluster on startup
- the richer browser-flow policy proof for observer, operator, and admin groups is still being hardened against the current local Keycloak `26.x` path on kind
- use OIDC for the product auth model today, but keep local kind browser-flow claims conservative until that focused proof is green again

Key values:

- `auth.oidc.discoveryUrl`
- `auth.oidc.clientId`
- `auth.oidc.clientSecret.*`
- `auth.oidc.claims.*`
- `authz.mode=externalClaimGroups`
- `authz.bootstrap.*`

## LDAP

LDAP is a first-class managed authentication option.

Use LDAP when you want:

- directory-backed login
- LDAP group or user provider integration
- NiFi-native LDAP auth wiring through the chart

Key values:

- `auth.ldap.*`
- `authz.mode=ldapSync`
- `authz.bootstrap.*`

## Support Level

- single-user: supported
- OIDC: supported, with conservative kind proof for the current browser-flow group-claims path
- LDAP: supported, with focused runtime proof on kind

What remains intentionally out of scope:

- human-user provisioning workflows
- broad identity management
- IdP write-back automation
