# Optional Service Mesh Profiles

These are optional install variants for teams that already run a supported service mesh profile in the cluster.

They are not the default install path. Start with [Install with Helm](helm.md) unless you already know you need one of these profiles.

## Before You Use These Profiles

All service mesh profiles in this repo stay scoped to the NiFi workload:

- they affect the NiFi workload only
- the controller stays outside the mesh
- they do not add mesh-specific controller behavior

You still start from the standard managed install values:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

Then add one optional overlay.

## Linkerd

Use this when you want the Linkerd-compatible NiFi workload profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-linkerd-values.yaml
```

Prerequisites:

- Linkerd is already installed and operated separately

## Istio Sidecar Mode

Use this when you want the Istio sidecar-mode NiFi workload profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-istio-values.yaml
```

Prerequisites:

- Istio is already installed and operated separately
- sidecar injection is enabled for the NiFi namespace only
- the controller namespace stays outside the mesh

## Istio Ambient

Use this when you want the Istio Ambient NiFi workload profile:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-istio-ambient-values.yaml
```

Prerequisites:

- Istio Ambient is already installed and operated separately

## Read Next

- [Install with Helm](helm.md)
- [Compatibility](../compatibility.md)
- [Operations and Troubleshooting](../operations.md)
