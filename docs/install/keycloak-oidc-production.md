# Keycloak OIDC Production Setup

Use this guide when:

- Keycloak is customer-managed
- NiFi-Fabric is using `auth.mode=oidc`
- you want a clear first-install checklist without dev bootstrap shortcuts

This is the recommended production model.

Keycloak remains the source of truth for:

- users
- passwords
- group membership
- realm and client configuration

NiFi-Fabric remains the source of truth for:

- the NiFi group catalog that must exist
- group-to-bundle mappings
- the bootstrap admin group used by NiFi

## What You Need Before Install

You need:

1. A Keycloak realm for NiFi
2. A confidential OIDC client for NiFi
3. A groups claim mapper on that client
4. The Keycloak groups that NiFi-Fabric will seed and bind
5. At least one initial admin user in the bootstrap admin group
6. Kubernetes `Secret/nifi-oidc` containing the client secret
7. NiFi-Fabric values that match the Keycloak realm and group model

## Step 1: Create the Realm

Create a realm, for example:

- `nifi`

Use that same realm name in the NiFi discovery URL:

```yaml
nifi:
  auth:
    oidc:
      discoveryUrl: https://idp.example.com/realms/nifi/.well-known/openid-configuration
```

## Step 2: Create the OIDC Client

Create a confidential OpenID Connect client for NiFi.

Recommended shape:

- `clientId`: `nifi-fabric`
- protocol: `openid-connect`
- confidential client
- standard authorization code flow enabled

Set redirect URIs and web origins to the public NiFi host that users will actually use in the browser.

Example shape:

- redirect URI: `https://nifi.example.com/*`
- web origin: `https://nifi.example.com`

That same public host must appear in `nifi.web.proxyHosts`.

## Step 3: Configure the Groups Mapper

Add a groups mapper to the client.

Recommended default:

- claim name: `groups`
- include groups in the ID token
- include groups in the access token
- emit simple names, not full paths, unless you intentionally want path mode

If Keycloak emits simple names:

- `nifi-platform-admins`
- `nifi-flow-operators`

Then NiFi-Fabric should use:

```yaml
claims:
  groups: groups
```

And the generated or declared NiFi authz overlay should use those same group values.

If Keycloak emits full paths instead, keep that explicit and make the NiFi-side group values match.

## Step 4: Create the Bootstrap Groups

Create the Keycloak groups that NiFi-Fabric will seed into NiFi.

Recommended starter set:

- `nifi-platform-admins`
- `nifi-flow-operators`
- `nifi-flow-observers`

These group names must match the NiFi-Fabric authz overlay exactly.

## Step 5: Create the First Admin User

Create at least one real user in Keycloak and add that user to the bootstrap admin group.

Recommended:

- put the first admin in `nifi-platform-admins`
- do not rely on a baked-in shared default user in production

NiFi-Fabric then uses:

```yaml
authz:
  bootstrap:
    initialAdminGroup: nifi-platform-admins
```

## Step 6: Create `Secret/nifi-oidc`

The OIDC client secret for the Keycloak client must be stored in Kubernetes as:

- Secret name: `nifi-oidc`
- key: `clientSecret`

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nifi-oidc
type: Opaque
stringData:
  clientSecret: <the-keycloak-client-secret>
```

Or:

```bash
kubectl -n nifi create secret generic nifi-oidc \
  --from-literal=clientSecret='<the-keycloak-client-secret>'
```

This is the Keycloak client secret for the NiFi OIDC client.

It is not:

- a user password
- the Keycloak admin password
- a token

## Step 7: Install NiFi-Fabric

Use the production overlay:

- [values-prod-oidc.yaml](../../examples/values-prod-oidc.yaml)

Install with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/values-prod-oidc.yaml
```

Make sure you update:

- `nifi.auth.oidc.discoveryUrl`
- `nifi.auth.oidc.clientId`
- `nifi.web.proxyHosts`

If your IdP uses a private CA, also configure the additional trust bundle as shown in [values-prod-oidc.yaml](../../examples/values-prod-oidc.yaml).

## Step 8: First Login Check

After install:

1. Open NiFi at the real public HTTPS host
2. Log in with the Keycloak admin user you placed in the bootstrap admin group
3. Confirm the user has admin access

If login works but admin access does not, check:

- the Keycloak group claim shape
- the exact group names
- `authz.bootstrap.initialAdminGroup`

## Ongoing User Management

After first install:

- create users in Keycloak
- change passwords in Keycloak
- add users to existing seeded groups in Keycloak

NiFi-Fabric does not reset Keycloak passwords and does not reconcile Keycloak user state.

### What Does Not Require a NiFi Restart

- adding a new user to an already-seeded Keycloak group
- changing a user password in Keycloak

### What Usually Requires Only Re-Login

- changing group membership for a user who is already logged in

The user should re-login so NiFi sees fresh claims.

### What Requires a NiFi Authz Update and Rollout

- adding a brand-new Keycloak group name that NiFi-Fabric has not seeded yet
- changing group-to-bundle mappings
- changing the bootstrap admin group

In those cases:

1. update the mapping or values
2. regenerate the authz overlay if you use the authz tool
3. redeploy NiFi-Fabric

## Source of Truth Summary

Keycloak console or IdP process:

- users
- passwords
- membership
- realm and client settings

Git and Helm:

- NiFi group catalog
- bundle bindings
- bootstrap admin group

There is no password resync from NiFi-Fabric back into Keycloak.

If you use a Keycloak bootstrap or import process that reapplies users repeatedly, any password overwrite behavior comes from that Keycloak-side process, not from NiFi-Fabric.

## Related

- [Authentication](../manage/authentication.md)
- [Advanced Install Paths](advanced.md)
- [NiFi-Fabric Authz Tooling](../../tools/nifi-fabric-authz/README.md)
