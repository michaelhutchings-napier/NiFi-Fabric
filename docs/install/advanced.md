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

## Kustomize

A separate customer-facing kustomize install bundle is not shipped in this slice.

That is intentional:

- the product architecture stays centered on Helm
- `charts/nifi-platform` remains the primary customer install surface
- a new install wrapper should only be added if it improves customer UX without creating a parallel product story

## When to Use an Advanced Path

Use an advanced path when you need:

- standalone NiFi without the controller
- separate controller and workload release boundaries
- lower-level platform integration work
- custom GitOps assembly beyond the standard one-release chart
