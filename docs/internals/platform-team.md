# Platform Team Notes

This page is for lower-level platform-team workflows. It is not part of the standard customer install story.

## Standalone Chart

Use `charts/nifi` when you want the reusable app chart without the managed controller path:

```bash
helm upgrade --install nifi charts/nifi \
  --namespace nifi \
  --create-namespace \
  -f examples/standalone/values.yaml
```

## Manual Managed Assembly

If you want to assemble the managed path in separate steps, use:

- `charts/nifi`
- the CRD in `config/crd/bases/platform.nifi.io_nificlusters.yaml`
- the controller manifests in `config/`
- the example `NiFiCluster` manifests in `examples/managed/`

This is useful for lower-level platform integration work, but it is not the recommended customer entrypoint.
