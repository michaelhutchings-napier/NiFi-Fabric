# LDAP Production Setup

Use this guide when:

- your directory is customer-managed
- NiFi-Fabric is using `auth.mode=ldap`
- you want a clear first-install checklist for the current `ldap + ldapSync` path

This is the current production LDAP model.

LDAP remains the source of truth for:

- users
- passwords
- group membership
- directory object structure

NiFi-Fabric remains the source of truth for:

- the explicit bootstrap admin identity or group
- the bounded NiFi-side authorization settings allowed by `ldapSync`

## Current Scope

The current LDAP path is intentionally narrower than the OIDC `externalClaimGroups` path.

Today:

- `auth.mode=ldap` requires `authz.mode=ldapSync`
- LDAP supports the explicit bootstrap admin path through `authz.bootstrap.*`
- LDAP does not currently support the richer named bundle mapping model used by OIDC
- the baseline `ldap-values.yaml` example stays on `initialAdminIdentity`
- LDAP initial-admin-group bootstrap is available on newer NiFi images and is the right advanced path when directory groups should drive first admin access

So the main production value is:

- native directory-backed login
- native directory-backed user and group lookup
- explicit first-admin bootstrap

## What You Need Before Install

You need:

1. A reachable LDAP or AD server
2. Bind credentials for NiFi to search the directory
3. User search settings
4. Group search settings
5. TLS trust configured if using LDAPS or START_TLS
6. An explicit bootstrap admin identity or bootstrap admin group
7. Kubernetes `Secret/nifi-ldap-bind` with the bind DN and password
8. NiFi-Fabric values that match the real directory schema

## Step 1: Create `Secret/nifi-ldap-bind`

Store the LDAP manager or bind credentials in Kubernetes as:

- Secret name: `nifi-ldap-bind`
- key `managerDn`
- key `managerPassword`

Example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: nifi-ldap-bind
type: Opaque
stringData:
  managerDn: cn=admin,dc=example,dc=com
  managerPassword: <ldap-bind-password>
```

Or:

```bash
kubectl -n nifi create secret generic nifi-ldap-bind \
  --from-literal=managerDn='cn=admin,dc=example,dc=com' \
  --from-literal=managerPassword='<ldap-bind-password>'
```

## Step 2: Configure the LDAP Connection

Set the directory URL and authentication strategy to match your environment.

Common shapes:

- `ldaps://ldap.example.com:636` with `authenticationStrategy: LDAPS`
- `ldap://ldap.example.com:389` with `authenticationStrategy: START_TLS`
- plain LDAP only for controlled internal or test environments

Match the chart values to the real directory:

```yaml
auth:
  mode: ldap
  ldap:
    url: ldaps://ldap.example.com:636
    authenticationStrategy: LDAPS
    managerSecret:
      name: nifi-ldap-bind
      dnKey: managerDn
      passwordKey: managerPassword
```

## Step 3: Configure User Search

NiFi needs to find users in the directory.

Typical settings:

```yaml
userSearch:
  base: ou=People,dc=example,dc=com
  filter: (uid={0})
  scope: SUBTREE
  objectClass: inetOrgPerson
  identityAttribute: uid
```

Match these to your real directory schema.

The most important values are:

- `base`
- `filter`
- `identityAttribute`

If these are wrong, login may succeed inconsistently or the user identity may not match what NiFi expects.

## Step 4: Configure Group Search

NiFi also needs to resolve directory groups for the `ldapSync` path.

Typical settings:

```yaml
groupSearch:
  base: ou=Groups,dc=example,dc=com
  scope: SUBTREE
  objectClass: groupOfNames
  nameAttribute: cn
  memberAttribute: member
```

In some directories you may also need:

- `memberReferencedUserAttribute`

That is common when group membership points to a user DN but NiFi needs a specific user attribute for resolution.

## Step 5: Choose the Bootstrap Admin Path

LDAP requires an explicit first-admin path.

Use one of:

- `authz.bootstrap.initialAdminIdentity`
- `authz.bootstrap.initialAdminGroup`

Use `authz.bootstrap.initialAdminIdentity` for the baseline path on the default `apache/nifi:2.0.0` image.

Use `authz.bootstrap.initialAdminGroup` on newer NiFi images when you want the directory group to drive first admin access. This path requires Apache NiFi `>= 2.5.0`, and it is production-proven here on `apache/nifi:2.8.0`.

Examples:

```yaml
authz:
  mode: ldapSync
  bootstrap:
    initialAdminIdentity: alice
```

Or:

```yaml
authz:
  mode: ldapSync
  bootstrap:
    initialAdminGroup: nifi-platform-admins
```

## Step 6: Install NiFi-Fabric

Use the LDAP overlay:

- [ldap-values.yaml](../../examples/ldap-values.yaml)

Or, for the newer NiFi group-bootstrap path:

- [ldap-group-bootstrap-values.yaml](../../examples/ldap-group-bootstrap-values.yaml)
- [nifi-2.8.0-values.yaml](../../examples/nifi-2.8.0-values.yaml)

Install with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/ldap-values.yaml
```

Update the LDAP values to match your real environment before install.

For group bootstrap on newer NiFi images:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/nifi-2.8.0-values.yaml \
  -f examples/ldap-group-bootstrap-values.yaml
```

## Step 7: First Login Check

After install:

1. Open NiFi
2. Log in with the directory user or admin identity you chose for bootstrap
3. Confirm the admin can access the expected NiFi UI and API areas

If first login works but admin access does not, check:

- bootstrap identity or group spelling
- user identity attribute
- group search configuration

## Ongoing User Management

After first install:

- create users in LDAP or AD
- change passwords in LDAP or AD
- change group membership in LDAP or AD

NiFi-Fabric does not reset directory passwords and does not reconcile directory user state.

### What Usually Does Not Require a NiFi Restart

- adding a user in the directory
- changing a user password
- changing membership in existing directory groups

LDAP-driven changes should appear according to the configured sync behavior.

The main control is:

- `auth.ldap.syncInterval`

So the practical expectation is:

- change in directory
- wait for sync interval or next relevant auth lookup
- user logs in or retries

Production note:

- group-bootstrap and post-start membership changes depend on the LDAP user-group provider, not just the login provider
- keep the login `userSearch.filter` as your normal bind/login lookup
- NiFi-Fabric now defaults the LDAP user-group-provider user search filter automatically:
  - baseline identity bootstrap inherits the login filter
  - group bootstrap defaults to a blank user-group-provider filter
- only override the user-group-provider filter when you have a strong directory-specific reason
- reusing a login-shaped filter like `(uid={0})` in the user-group provider can prevent group-based authorization from resolving correctly

### What Still Requires a NiFi Values Change and Rollout

- changing the bootstrap admin identity
- changing the bootstrap admin group
- changing bounded NiFi-side policy intent for the LDAP path
- changing LDAP connection, bind, search, or trust settings

## Source of Truth Summary

Directory:

- users
- passwords
- membership
- directory object model

Git and Helm:

- NiFi LDAP connection settings
- explicit bootstrap admin path
- bounded NiFi-side LDAP authorization settings

There is no password resync from NiFi-Fabric back into LDAP.

## Common Failure Modes

Bind failure:

- wrong `managerDn`
- wrong bind password
- bind account lacks search permissions

Search mismatch:

- wrong user search base
- wrong user filter
- wrong group search base
- wrong object class or membership attribute

Identity mismatch:

- `identityAttribute` does not match the username form users actually enter
- case handling differs from what the directory returns

TLS trust failure:

- NiFi does not trust the LDAP server certificate
- START_TLS or LDAPS handshake fails

Nested group assumptions:

- directory groups may be nested but the configured search attributes do not resolve membership the way operators expect

## Related

- [External Identity Providers](external-identity-providers.md)
- [Authentication](../manage/authentication.md)
- [Advanced Install Paths](advanced.md)
- [ldap-values.yaml](../../examples/ldap-values.yaml)
- [ldap-group-bootstrap-values.yaml](../../examples/ldap-group-bootstrap-values.yaml)
