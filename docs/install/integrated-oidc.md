# Integrated OIDC Install Contract

This document describes an advanced higher-level install pattern for teams that want Keycloak and NiFi-Fabric to come up together from one install flow while keeping the core NiFi-Fabric charts unchanged.

This is intentionally not the standard path.

Use it when:

- you want one install workflow to provision both the identity-provider bootstrap inputs and the NiFi OIDC/authz inputs
- you want the OIDC client secret declared once and used in both Keycloak and NiFi
- you still want NiFi-Fabric to stay out of Keycloak lifecycle ownership at runtime

Do not use it as the default first experience for customers who already have an existing IdP team or customer-managed Keycloak process.

## Boundary

The existing NiFi-Fabric chart contract stays the same:

- `auth.mode=oidc`
- `auth.oidc.discoveryUrl`
- `auth.oidc.clientId`
- `auth.oidc.clientSecret.existingSecret`
- `authz.mode=externalClaimGroups`
- `authz.bootstrap.initialAdminGroup`
- `authz.applicationGroups`
- `authz.bundles.*`
- explicit `web.proxyHosts`

The integrated path lives above that contract.

It can be implemented as:

- an umbrella chart
- a platform installer chart
- a GitOps bundle with a Keycloak bootstrap Job plus `charts/nifi-platform`

NiFi-Fabric still does not become a Keycloak operator.

## One Secret, Two Consumers

The key design rule is:

- do not wait for Keycloak to generate a client secret and then copy it back out
- declare the OIDC client secret once
- use that same value in both places

That means the higher-level install path should create or reference:

- `Secret/nifi-oidc`

With key:

- `clientSecret`

Keycloak client bootstrap then uses that same value when creating the confidential OIDC client.

NiFi-Fabric uses the same Secret through the existing `auth.oidc.clientSecret.existingSecret` contract.

This keeps the install deterministic and avoids hidden post-start discovery or sync logic.

## Ownership Model

Keycloak remains the source of truth for:

- users
- passwords
- user lifecycle
- group membership
- realm, client, and mapper configuration

Git and Helm remain the source of truth for:

- the NiFi group catalog that must be seeded
- group-to-bundle mappings
- the bootstrap admin group
- ingress or Route hostnames

NiFi-Fabric does not reconcile Keycloak user state and does not reset Keycloak passwords.

## Expected Keycloak Shape

Before first real NiFi login, the identity side should contain:

- a realm for NiFi
- a confidential OIDC client for NiFi
- a groups claim mapper
- bootstrap groups such as:
  - `nifi-platform-admins`
  - `nifi-flow-operators`
  - `nifi-flow-observers`
- at least one bootstrap admin user in the bootstrap admin group

The claim value shape must match the generated NiFi authz overlay:

- simple names such as `nifi-flow-operators`, or
- full paths such as `/platform/nifi-flow-operators`

See [NiFi-Fabric Authz Tooling](../../tools/nifi-fabric-authz/README.md) for the `groupValueMode` guidance and validation flow.

## Example Higher-Level Contract

The following example is a higher-level values contract for an umbrella or installer layer. It is not consumed directly by `charts/nifi-platform` today.

```yaml
identity:
  keycloak:
    enabled: true
    bootstrap:
      enabled: true
      realm: nifi

      client:
        id: nifi-fabric
        secretRef:
          name: nifi-oidc
          key: clientSecret

      groups:
      - nifi-platform-admins
      - nifi-flow-operators
      - nifi-flow-observers

      bootstrapAdmin:
        enabled: true
        username: alice
        passwordSecretRef:
          name: keycloak-bootstrap-admin
          key: password
        groups:
        - nifi-platform-admins

nifi:
  auth:
    mode: oidc
    oidc:
      discoveryUrl: https://keycloak.example.com/realms/nifi/.well-known/openid-configuration
      clientId: nifi-fabric
      clientSecret:
        existingSecret: nifi-oidc
        key: clientSecret
      claims:
        identifyingUser: email
        groups: groups

  authz:
    mode: externalClaimGroups
    bootstrap:
      initialAdminGroup: nifi-platform-admins
    applicationGroups:
    - nifi-platform-admins
    - nifi-flow-operators
    - nifi-flow-observers
    bundles:
      admin:
        groups:
        - nifi-platform-admins
      viewer:
        groups:
        - nifi-flow-operators
        - nifi-flow-observers
      editor:
        groups:
        - nifi-flow-operators
      flowVersionManager:
        groups:
        - nifi-flow-operators
```

## Recommended Install Sequencing

The higher-level install layer should do this:

1. Create or reference `Secret/nifi-oidc`.
2. Bootstrap Keycloak realm, client, groups, and optional initial admin user using that same client secret.
3. Install NiFi-Fabric through `charts/nifi-platform` with the existing OIDC contract.
4. Allow first login only after Keycloak and NiFi bootstrap inputs are both ready.

This can happen in one release workflow, but the readiness must still be explicit.

## Runtime Behavior After Install

After installation:

- new users can be created in Keycloak without restarting NiFi
- adding users to already-seeded groups does not require a NiFi restart
- users who already have an active session should re-login after group membership changes so NiFi sees fresh claims
- new Keycloak group names still require a regenerated NiFi authz overlay and rollout

Passwords remain Keycloak-owned.

If temporary passwords are changed in the Keycloak console, NiFi-Fabric does not revert them.

If a separate Keycloak bootstrap or import process reapplies users and passwords repeatedly, that behavior is Keycloak-side lifecycle ownership, not NiFi-Fabric reconciliation.

## Production Recommendation

For production:

- prefer `Secret/nifi-oidc` from an external secret manager, sealed secret, or pre-created Secret
- prefer customer-owned Keycloak lifecycle even if the install is wrapped by a higher-level platform chart
- keep the integrated bootstrap path clearly labeled as an advanced convenience option, not the default platform contract

## Related

- [Advanced Install Paths](advanced.md)
- [Authentication](../manage/authentication.md)
- [NiFi-Fabric Authz Tooling](../../tools/nifi-fabric-authz/README.md)
