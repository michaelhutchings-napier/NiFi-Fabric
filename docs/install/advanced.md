# Advanced Install Paths

NiFi-Fabric keeps the standard install path simple and cert-manager-first. Advanced paths are available when you want explicit ownership of auth or TLS inputs.

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
