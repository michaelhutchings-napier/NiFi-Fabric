# Advanced Install Paths

NiFi-Fabric keeps the standard install path simple. Advanced paths are available, but they are secondary.

## Supported Secondary Paths

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
- no separate kustomize wrapper is shipped, because that would either duplicate chart logic or depend on Helm-enabled kustomize behavior that is less predictable for customers

## When to Use an Advanced Path

Use an advanced path when you need:

- standalone NiFi without the controller
- separate controller and workload release boundaries
- lower-level platform integration work
- manifest-based GitOps assembly beyond the standard one-release chart
