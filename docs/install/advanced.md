# Advanced Install Paths

NiFi-Fabric keeps the standard install path simple and cert-manager-first. Advanced paths are available when you want explicit ownership of auth or TLS inputs.

If you are not trying to keep full Secret ownership, stop here and use the standard install path in [Install with Helm](helm.md).

## Advanced Managed Install

Use the advanced managed path when you want to bring your own Secrets.

### External TLS Secret Ownership

Use this when you want explicit operator-provided auth and TLS Secrets in the release namespace:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

Before install, create:

- `Secret/nifi-auth`
- `Secret/nifi-tls`

If you enable `repositoryEncryption.*`, also create:

- `Secret/nifi-repository-encryption`

The default `nifi-tls` contract is:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- `keystorePassword`
- `truststorePassword`
- `sensitivePropsKey`

Examples:

- [`examples/secret-contracts/auth-single-user-secret.yaml`](/tmp/tmp.ZKzaVUztym/examples/secret-contracts/auth-single-user-secret.yaml)
- [`examples/secret-contracts/tls-external-secret.yaml`](/tmp/tmp.ZKzaVUztym/examples/secret-contracts/tls-external-secret.yaml)
- [`examples/secret-contracts/repository-encryption-secret.yaml`](/home/michael/Work/nifi2-platform/examples/secret-contracts/repository-encryption-secret.yaml)

### Explicit Cert-Manager Inputs

Use this when cert-manager already exists, but you still want explicit ownership of the supporting auth and parameter Secrets:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-values.yaml
```

Before install, create:

- `Secret/nifi-auth`
- `Secret/nifi-tls-params`

The default `nifi-tls-params` contract is:

- `pkcs12Password`
- `sensitivePropsKey`

cert-manager still writes `Secret/nifi-tls` from the rendered `Certificate`.

Examples:

- [`examples/secret-contracts/auth-single-user-secret.yaml`](/tmp/tmp.ZKzaVUztym/examples/secret-contracts/auth-single-user-secret.yaml)
- [`examples/secret-contracts/tls-cert-manager-params-secret.yaml`](/tmp/tmp.ZKzaVUztym/examples/secret-contracts/tls-cert-manager-params-secret.yaml)

You still install separately:

- cert-manager
- the referenced issuer or `ClusterIssuer`

### Quickstart To Explicit Cert-Manager Handoff

If you started with the standard cert-manager quickstart path and want to move to the explicit cert-manager path without changing Secret names, upgrade in place to [`examples/platform-managed-cert-manager-values.yaml`](/home/michael/Work/nifi2-platform/examples/platform-managed-cert-manager-values.yaml).

The chart preserves the previously generated quickstart `nifi-auth` and `nifi-tls-params` Secrets during that handoff when the explicit path still points at those same names.

This keeps the upgrade stable while making the input ownership explicit in values.

### OIDC and LDAP

OIDC and LDAP remain first-class supported auth modes, but they fit the advanced path rather than the standard single-user bootstrap path.

That means:

- no fake `singleUser` bootstrap Secret is required
- IdP or LDAP-specific Secrets stay explicit and operator-owned
- `Initial Admin Identity` or `Initial Admin Group` remains explicit

For advanced managed installs:

- start from the explicit managed path
- add the equivalent `nifi.auth.*` and `nifi.authz.*` values for OIDC or LDAP
- keep the provider-specific Secrets and bootstrap admin settings explicit

See [Authentication](../manage/authentication.md) for the auth-mode details and supported value shapes.
Use [External Identity Providers](external-identity-providers.md) when you want
the customer-facing OIDC, Keycloak, and LDAP setup contract in one place.

Optional higher-level platform integration is also possible for OIDC:

- keep the NiFi-Fabric chart contract unchanged
- declare the OIDC client secret once
- use that same secret in both Keycloak bootstrap and `Secret/nifi-oidc`
- keep this as an advanced install option above `charts/nifi-platform`, not as a change to the core chart contract

See:

- [External Identity Providers](external-identity-providers.md)
- [Integrated OIDC Install Contract](integrated-oidc.md)
- [Keycloak OIDC Production Setup](keycloak-oidc-production.md)

#### OIDC Dev Bootstrap Track

Use this when you want a local, personal, or demo-friendly OIDC workflow with a small Keycloak realm bootstrap and an optional dev admin user.

Recommended sequencing:

1. Bootstrap Keycloak with a small realm import.
2. Create `Secret/nifi-oidc` with the same client secret used by the imported client.
3. Install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/values-dev-keycloak-bootstrap.yaml
```

Use:

- [values-dev-keycloak-bootstrap.yaml](../../examples/values-dev-keycloak-bootstrap.yaml)
- [keycloak-dev-bootstrap-realm.json](../../examples/keycloak-dev-bootstrap-realm.json)

This path is convenience-oriented and intentionally non-production.

#### OIDC Production Track

Use this for the recommended customer OIDC workflow.

Required sequencing:

1. The customer completes Keycloak realm, groups, users, and OIDC client setup first.
2. The bootstrap admin group exists in Keycloak before NiFi first login.
3. Create `Secret/nifi-oidc` with the customer-managed client secret.
4. Install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/values-prod-oidc.yaml
```

Use:

- [values-prod-oidc.yaml](../../examples/values-prod-oidc.yaml)
- [External Identity Providers](external-identity-providers.md)
- [Keycloak OIDC Production Setup](keycloak-oidc-production.md)

This is the recommended customer ownership model because Keycloak owns realm, client, user, and group administration while NiFi-Fabric only needs the matching authz scaffold ready before first login.

Operational note:

- users can be created in Keycloak after NiFi is already running
- adding those users to existing seeded groups does not require a NiFi restart
- already logged-in users should re-login after membership changes so NiFi sees fresh claims
- new group names still require an authz overlay update and rollout

#### LDAP Production Track

Use this for the current production LDAP workflow.

Required sequencing:

1. The customer confirms real LDAP or AD connectivity, bind credentials, user search, and group search settings first.
2. The explicit bootstrap admin identity or bootstrap admin group is chosen up front.
3. Create `Secret/nifi-ldap-bind` with the bind DN and password.
4. Install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/ldap-values.yaml
```

Use:

- [ldap-values.yaml](../../examples/ldap-values.yaml)
- [External Identity Providers](external-identity-providers.md)
- [LDAP Production Setup](ldap-production.md)

Current limitation:

- the LDAP path is intentionally narrower than OIDC `externalClaimGroups`
- `ldap + ldapSync` currently focuses on native directory login plus the explicit bootstrap admin path
- the baseline example uses `authz.bootstrap.initialAdminIdentity` on the default image
- `authz.bootstrap.initialAdminGroup` is available on newer NiFi images and should be treated as an advanced option

#### Advanced Integrated OIDC Option

Use this only when you want a higher-level install path that bootstraps both Keycloak and NiFi-Fabric together while keeping the NiFi chart contract stable.

Recommended shape:

1. A higher-level install layer creates or references `Secret/nifi-oidc`.
2. That same secret value is used when bootstrapping the Keycloak OIDC client.
3. `charts/nifi-platform` still consumes the existing OIDC values contract unchanged.

This is intentionally:

- advanced
- optional
- install-time only

It is intentionally not:

- a change to the core NiFi-Fabric OIDC contract
- runtime secret discovery from Keycloak
- Keycloak reconciliation by NiFi-Fabric

See:

- [Integrated OIDC Install Contract](integrated-oidc.md)
- [integrated-keycloak-oidc-contract.yaml](../../examples/integrated-keycloak-oidc-contract.yaml)

## Generated Manifest Bundle

If you need a manifest-based workflow without copying chart logic, render a generated bundle from `charts/nifi-platform`:

```bash
make render-platform-managed-bundle
kubectl apply -f dist/nifi-platform-managed-bundle.yaml
```

Cert-manager variant:

```bash
make render-platform-managed-cert-manager-bundle
kubectl apply -f dist/nifi-platform-managed-cert-manager-bundle.yaml
```

Optional standalone variant:

```bash
make render-platform-standalone-bundle
kubectl apply -f dist/nifi-platform-standalone-bundle.yaml
```

## Fresh Packaged Platform Chart

If a downstream chart or GitOps repo needs a packaged `nifi-platform` archive, build it from a fresh temporary chart copy instead of relying on whatever nested `.tgz` files already exist in the workspace:

```bash
make package-platform-chart
```

That writes a newly packaged chart into `dist/charts` and rebuilds nested dependencies first, which avoids stale bundled subchart archives during local downstream integration.

For standalone chart use and lower-level platform-team assembly, see [Platform Team Notes](../internals/platform-team.md).
For backup and recovery, see [Backup, Restore, and Disaster Recovery](../dr.md).

## When to Use an Advanced Path

Use an advanced path when you need:

- explicit auth or TLS Secret ownership
- OIDC or LDAP with explicit identity-provider inputs
- generated manifest workflows
- manifest-based GitOps assembly beyond the standard one-release chart
