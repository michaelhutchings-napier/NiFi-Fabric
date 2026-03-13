# NiFi-Fabric

NiFi-Fabric is a Kubernetes platform layer for Apache NiFi 2.x.

It provides a product-facing one-release install path through `charts/nifi-platform`, a reusable standalone app chart in `charts/nifi`, and a thin controller for lifecycle and safety decisions that Helm cannot perform safely on its own.

## Why NiFi-Fabric

- one Helm release is the standard customer install path
- the controller stays focused on safe rollout, TLS restart policy, hibernation, restore, and controller-owned autoscaling
- NiFi-native behavior stays in NiFi, standard Kubernetes resources stay in Helm
- OIDC and LDAP are first-class managed authentication options
- cert-manager is supported when it already exists in the cluster
- Git-based Flow Registry Clients are the supported modern direction
- observability and metrics are a first-class subsystem instead of an afterthought

## Standard Install Path

The standard customer path is `charts/nifi-platform`.

Prerequisites:

- a reachable controller image for the target cluster
- `Secret/nifi-tls`
- `Secret/nifi-auth`
- cert-manager only when you choose cert-manager TLS mode

Managed platform install:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml
```

Managed platform install with cert-manager:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-cert-manager-values.yaml
```

## Documentation

Start here:

- [Docs Home](docs/README.md)
- [Start Here](docs/start-here.md)
- [Features](docs/features.md)
- [Compatibility](docs/compatibility.md)

Install:

- [Install with Helm](docs/install/helm.md)
- [Advanced Install Paths](docs/install/advanced.md)

Manage NiFi:

- [TLS and cert-manager](docs/manage/tls-and-cert-manager.md)
- [Authentication](docs/manage/authentication.md)
- [Autoscaling](docs/manage/autoscaling.md)
- [Flow Registry Clients](docs/manage/flow-registry-clients.md)
- [Hibernation and Restore](docs/manage/hibernation-and-restore.md)
- [Observability and Metrics](docs/manage/observability.md)

Reference and support:

- [Architecture Summary](docs/architecture.md)
- [Verification and Support Levels](docs/testing.md)
- [NiFiCluster Reference](docs/reference/nificluster.md)
- [Platform Chart Values Reference](docs/reference/nifi-platform-values.md)
- [App Chart Values Reference](docs/reference/nifi-values.md)
- [Operations and Troubleshooting](docs/operations.md)
- [Experimental Features](docs/experimental-features.md)
- [Roadmap](docs/roadmap.md)

## Compatibility Summary

NiFi-Fabric targets Apache NiFi `2.0.0` through `2.8.x`.

- focused runtime proof today covers `apache/nifi:2.0.0` and `apache/nifi:2.8.0`
- other NiFi `2.x` versions are expected to work unless noted, but are not yet runtime-proven in this repository
- NiFi `1.x` is not supported
- AKS is a primary target, but current repo proof is still kind-first
- OpenShift is supported as a prepared secondary target and remains conservative until real-cluster proof is recorded

See [Compatibility](docs/compatibility.md) for the detailed matrix.

## Product Position

- `charts/nifi-platform` is the standard product install
- `charts/nifi` is the standalone-capable app chart
- built-in controller-owned autoscaling is the primary autoscaling model
- KEDA is optional, experimental, and secondary as an external intent source
- native API metrics are the primary supported metrics path
- exporter metrics are experimental
- site-to-site metrics are prepared-only

## Experimental Features

These features are available but intentionally marked experimental:

- controller-owned enforced autoscaling scale-down
- KEDA integration
- exporter metrics mode

Prepared-only, not runtime-enabled:

- site-to-site metrics mode

## Install Surface Note

A separate customer-facing kustomize install bundle is not shipped in this slice.

The supported install surfaces remain:

- `charts/nifi-platform` for the standard one-release platform path
- `charts/nifi` for standalone or advanced assembly

## Conservative Claims

NiFi-Fabric documentation is intentionally conservative in a few areas:

- AKS and OpenShift guidance is published, but real-cluster runtime proof is not yet claimed here
- KEDA is documented as experimental even though focused kind proof is green
- autoscaling scale-down remains intentionally one-step-at-a-time and experimental
- exporter metrics are experimental
- site-to-site metrics remain prepared-only
