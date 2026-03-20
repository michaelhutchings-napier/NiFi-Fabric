# NiFi-Fabric

NiFi-Fabric is a Kubernetes platform for Apache NiFi 2.x.

It provides a standard product install path through `charts/nifi-platform`, keeps ordinary Kubernetes resources in Helm, and uses a thin controller for the lifecycle and safety work that Helm cannot do safely on its own.

## Why NiFi-Fabric

- one clear Helm install path for the standard managed deployment
- secure-by-default, cert-manager-first installation
- safe lifecycle handling for rollout, TLS restart policy, hibernation, restore, and controller-owned autoscaling
- first-class managed authentication options, including OIDC and LDAP
- native NiFi 2 Prometheus metrics support through direct secured API scraping
- a simpler product surface than a large NiFi-specific operator stack

## Standard Install

The standard customer path is `charts/nifi-platform`.

Install cert-manager first, create or choose the `Issuer` or `ClusterIssuer`, then install NiFi-Fabric with:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-cert-manager-quickstart-values.yaml
```

This standard path does not require pre-created bootstrap auth or TLS Secrets. The install bootstraps what it needs, and cert-manager creates the final workload TLS Secret.

## Documentation

Start here:

- [Start Here](docs/start-here.md)
- [Install with Helm](docs/install/helm.md)
- [Advanced Install Paths](docs/install/advanced.md)

Manage NiFi:

- [TLS and cert-manager](docs/manage/tls-and-cert-manager.md)
- [Authentication](docs/manage/authentication.md)
- [Observability and Metrics](docs/manage/observability-metrics.md)
- [Autoscaling](docs/manage/autoscaling.md)
- [Operations and Troubleshooting](docs/operations.md)

Reference and support:

- [Documentation Home](docs/README.md)
- [Architecture Summary](docs/architecture.md)
- [Compatibility](docs/compatibility.md)
- [Verification and Support Levels](docs/testing.md)
- [Experimental Features](docs/experimental-features.md)

Advanced install paths, support details, compatibility nuance, and verification coverage live in the docs rather than in this homepage.
