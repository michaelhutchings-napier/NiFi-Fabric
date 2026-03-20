# Advanced Install Paths

NiFi-Fabric keeps the standard install path simple and cert-manager-first. Advanced paths are available when you want explicit ownership of auth or TLS inputs.

## Explicit-Secret Managed Install

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

### OIDC and LDAP

OIDC and LDAP remain first-class supported auth modes, but they fit the advanced path rather than the standard single-user bootstrap path.

That means:

- no fake `singleUser` bootstrap Secret is required
- IdP or LDAP-specific Secrets stay explicit and operator-owned
- `Initial Admin Identity` or `Initial Admin Group` remains explicit

See [Authentication](../manage/authentication.md) for the auth-mode details.

## Secondary Paths

### Standalone App Chart

Use `charts/nifi` when you want the app chart without the managed controller path:

```bash
helm upgrade --install nifi charts/nifi \
  --namespace nifi \
  --create-namespace \
  -f examples/standalone/values.yaml
```

### Manual Managed Assembly

If you want to assemble the managed path in separate steps, use:

- `charts/nifi`
- the CRD in `config/crd/bases/platform.nifi.io_nificlusters.yaml`
- the controller manifests in `config/`
- the example `NiFiCluster` manifests in `examples/managed/`

This is useful for advanced platform teams, but it is not the recommended customer entrypoint.

### Generated Platform Bundle

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

This path stays secondary on purpose:

- the product architecture stays centered on Helm
- `charts/nifi-platform` remains the primary customer install surface
- the bundle is generated from the same chart and example overlays, so there is no second install architecture

## Control-Plane Backup Bundle

For production recovery planning, the repo also includes a thin control-plane export path:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

That bundle is not a second install architecture. It is an audit and recovery artifact for the existing Helm-centered install model.

Recover with:

```bash
bash hack/recover-control-plane-backup.sh \
  --backup-dir ./backup/nifi-control-plane
```

## When to Use an Advanced Path

Use an advanced path when you need:

- explicit auth or TLS Secret ownership
- OIDC or LDAP with explicit identity-provider inputs
- standalone NiFi without the controller
- lower-level platform integration work
- manifest-based GitOps assembly beyond the standard one-release chart
