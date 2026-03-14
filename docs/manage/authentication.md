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

## Mutable Flow Authorization Bootstrap

NiFi-Fabric supports one bounded mutable-flow authorization bundle for file-backed NiFi authorization modes.

Use it when you want chart-managed groups to:

- create and edit child process groups under the root canvas
- perform process-group-level version-control actions on descendants of that root canvas

Key values:

- `authz.capabilities.mutableFlow.enabled=true`
- `authz.capabilities.mutableFlow.includeInitialAdmin`
- `authz.capabilities.mutableFlow.groups[]`
- `authz.applicationGroups[]`

Support boundary:

- supported with `singleUser + fileManaged`
- supported with `oidc + externalClaimGroups`
- not supported with `ldap + ldapSync` in this slice

What it still does not try to do:

- per-user synchronization
- IdP write-back
- arbitrary policy automation
- controller-managed authorization changes

Group mapping stays unchanged:

- external identity groups map to seeded local NiFi groups
- the mutable-flow bundle binds inherited root-canvas `view` and `modify` process-group policies, plus supporting `flow` and `controller` read access, to those NiFi groups
- the bootstrap admin path can also receive the same bundle with `includeInitialAdmin=true`

Supported capabilities in this bounded profile:

- create child process groups under the root canvas
- edit and delete descendant process groups where NiFi evaluates inherited parent-group access
- view flow state needed for process-group editing
- start, stop, update, revert, and inspect process-group version-control state where NiFi authorizes those actions from process-group access

Intentionally excluded from this profile:

- tenant or policy administration
- arbitrary authorization automation
- parameter-context administration
- unrestricted controller write access

## Named Policy Bundles

NiFi-Fabric now provides a small named-bundle model as the recommended customer-facing path for common access profiles.

Available bundles:

- `viewer`: flow read access for browser and API observation
- `editor`: viewer access plus the bounded mutable-flow write bundle for process-group editing
- `flowVersionManager`: the current bounded version-control profile; today it maps to the same inherited process-group write surface as `editor`
- `admin`: the existing base admin policy set

Recommended use:

- bind external IdP groups to seeded NiFi groups with `authz.applicationGroups[]`
- assign those NiFi groups to `authz.bundles.<name>.groups[]`
- keep `authz.policies[]` for exceptions rather than as the default customer path
- keep `authz.capabilities.mutableFlow` for lower-level compatibility or advanced cases

Current support boundary:

- supported with `singleUser + fileManaged`
- supported with `oidc + externalClaimGroups`
- `ldap + ldapSync` keeps the bootstrap admin path, but non-admin named bundles are not chart-seeded in this slice

Workflow-oriented note:

- `flowVersionManager` is the bounded bundle intended for process-group editing plus process-group-level version-control work
- the current focused runtime proof uses that bundle for a GitHub save-to-registry workflow on NiFi `2.8.0`
- this does not turn the chart into a general-purpose authorization platform; advanced tenant, policy, and parameter administration remain separate concerns

The bundles compile down to the same underlying file-managed policies the chart already renders. They do not add a second authorization engine and they do not remove `authz.policies[]`.

## Support Level

- single-user: supported
- OIDC: supported, with conservative kind proof for the current browser-flow group-claims path
- LDAP: supported, with focused runtime proof on kind

What remains intentionally out of scope:

- human-user provisioning workflows
- broad identity management
- IdP write-back automation
