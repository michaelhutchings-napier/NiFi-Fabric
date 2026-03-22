# NiFi-Fabric: Apache NiFi 2 Kubernetes Platform

NiFi-Fabric is a straightforward Apache NiFi 2 platform for Kubernetes with a Helm-first install path and a thin operator for lifecycle safety.

It keeps ordinary Kubernetes resources in Helm, uses cert-manager-first TLS by default, and fits GitOps-style rollout on AKS, OpenShift, and other conformant Kubernetes environments.

It is built for teams that want a production-ready NiFi platform on Kubernetes without taking on a broad, CRD-heavy operator model.

AKS is the primary supported target environment. OpenShift is also supported.

## Fast Evaluation

For the fastest supported first install, run:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  --set global.nifiFabric.installProfile=quickstart-cert-manager \
  --set nifi.tls.certManager.issuerRef.name=nifi-ca
```

This assumes cert-manager is already installed and you have created or chosen the `Issuer` or `ClusterIssuer` your cluster will use for NiFi.

The install bootstraps the initial single-user login and TLS inputs, while cert-manager creates the final workload TLS Secret.

This quickstart uses the single-user bootstrap path. OIDC and LDAP stay available through the explicit install paths.

After install, continue with [First Access and Day-1 Checks](docs/first-day.md).

## Why NiFi-Fabric

- one clear Helm install path for the standard managed deployment
- secure-by-default, cert-manager-first installation
- safe lifecycle handling for rollout, TLS restart policy, hibernation, restore, and controller-owned autoscaling
- advisory and enforced autoscaling, plus optional KEDA integration
- first-class managed authentication options, including OIDC and LDAP
- runtime-managed NiFi configuration features, including Flow Registry Client catalogs, versioned-flow import, and Parameter Context management
- OpenShift `Route` support for external HTTPS access
- optional service mesh profiles for Linkerd, Istio sidecar mode, and Istio Ambient
- native NiFi 2 Prometheus metrics support through direct secured API scraping, including multiple named `ServiceMonitor` profiles and per-profile URL parameters
- optional exporter metrics and Site-to-Site delivery paths for metrics, status, and provenance
- optional trust-manager integration for shared CA bundle distribution across workload and observability paths
- a simpler product surface than a large NiFi-specific operator stack

## Recommended First Install

This quickstart command is the recommended first install for most teams using `charts/nifi-platform`.

It gives you a secure, low-friction managed install for evaluation and early bring-up. Teams can later move to explicit values files, OIDC or LDAP, and explicit enterprise auth or TLS ownership as part of production rollout planning.

See [Install with Helm](docs/install/helm.md) and [Advanced Install Paths](docs/install/advanced.md).

For local evaluation, `kind` is the primary documented path. `minikube` can also be used for small local installs when cert-manager, image access, and a working default `StorageClass` are available. See [Local Kind Guide](docs/local-kind.md).

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
- [Optional Service Mesh Profiles](docs/install/service-mesh.md)
- [Parameter Contexts](docs/manage/parameters.md)
- [Flows](docs/manage/flows.md)
- [Flow Registry Clients](docs/manage/flow-registry-clients.md)
- [Operations and Troubleshooting](docs/operations.md)

Reference and support:

- [Documentation Home](docs/README.md)
- [Contributing](CONTRIBUTING.md)
- [Architecture Summary](docs/architecture.md)
- [Compatibility](docs/compatibility.md)
- [Verification and Support Levels](docs/testing.md)
- [Experimental Features](docs/experimental-features.md)

For detailed install, compatibility, and verification guidance, use the docs.

Environment support:

- AKS is the primary supported target environment.
- OpenShift is supported, including OpenShift `Route` for external HTTPS access.

## License

NiFi-Fabric is licensed under the Apache License 2.0. See [LICENSE](LICENSE).

## Related Projects

NiFi-Fabric is a NiFi 2-first Kubernetes platform with a smaller product surface than a broad NiFi operator. It keeps the standard path Helm-first and cert-manager-first, uses a thin controller only for lifecycle and safety, and supports direct NiFi 2 Prometheus scraping. NiFiKop and the archived Cetic chart were useful reference points for install ergonomics and documentation style.

- [NiFiKop](https://konpyutaika.github.io/nifikop/docs/) is a broader NiFi Kubernetes operator with additional CRDs for cluster and dataflow management.
- [Cetic Helm Chart for Apache NiFi](https://github.com/cetic/helm-nifi) is an archived NiFi 1.x Helm chart.
