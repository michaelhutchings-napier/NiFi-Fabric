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
- OIDC: supported, with focused runtime proof on kind
- LDAP: supported, with focused runtime proof on kind

What remains intentionally out of scope:

- human-user provisioning workflows
- broad identity management
- IdP write-back automation
