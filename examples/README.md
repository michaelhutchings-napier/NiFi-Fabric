# Examples

These examples now cover both the product-facing platform chart and the lower-level app-chart or evaluator overlays.

Primary one-command product installs:

- standalone: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-standalone-values.yaml`
- managed: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-values.yaml`
- managed + cert-manager: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-cert-manager-values.yaml`

Generated manifest-bundle installs:

- managed: `make render-platform-managed-bundle && kubectl apply -f dist/nifi-platform-managed-bundle.yaml`
- managed + cert-manager: `make render-platform-managed-cert-manager-bundle && kubectl apply -f dist/nifi-platform-managed-cert-manager-bundle.yaml`

Advanced evaluator installs still exist:

- standalone: `make install-standalone`
- managed: `make install-managed`
- managed + cert-manager: `make install-managed-cert-manager`

There is also one AKS-prepared set of starting overlays:

- [aks/standalone-values.yaml](aks/standalone-values.yaml)
  - Prepared starting point for future AKS standalone evaluation.
  - Not yet validated on a real AKS cluster.

- [aks/managed-values.yaml](aks/managed-values.yaml)
  - Prepared starting point for future AKS managed-mode evaluation.
  - Compose with [cert-manager-values.yaml](cert-manager-values.yaml) if cert-manager already exists in the AKS cluster.
  - Not yet validated on a real AKS cluster.

There is also one OpenShift-prepared set of starting overlays:

- [openshift/standalone-values.yaml](openshift/standalone-values.yaml)
  - Prepared starting point for future OpenShift standalone evaluation.
  - Keeps the Service internal and renders a passthrough Route.
  - Not yet validated on a real OpenShift cluster.

- [openshift/managed-values.yaml](openshift/managed-values.yaml)
  - Prepared starting point for future OpenShift managed-mode evaluation.
  - Keeps the Service internal, renders a passthrough Route, and relaxes fixed kind-style UID settings.
  - Compose with [cert-manager-values.yaml](cert-manager-values.yaml) if cert-manager already exists in the OpenShift cluster.
  - Not yet validated on a real OpenShift cluster.

There is also one optional TLS-source overlay:

- [cert-manager-values.yaml](cert-manager-values.yaml)
  - Switches the chart from `tls.mode=externalSecret` to `tls.mode=certManager`.
  - Use it on top of either the standalone or managed Helm values when cert-manager and the `nifi-ca` issuer bootstrap are already installed.
  - Still requires a separate Secret for the PKCS12 password and `nifi.sensitive.props.key`.
  - For kind evaluator setup, run `make kind-bootstrap-cert-manager` first.
  - The focused fresh-kind evaluation commands are `make kind-cert-manager-e2e`, `make kind-cert-manager-fast-e2e`, `make kind-cert-manager-nifi-2-8-e2e`, and `make kind-cert-manager-nifi-2-8-fast-e2e`.

There is also one optional focused fast overlay:

- [test-fast-values.yaml](test-fast-values.yaml)
  - Reduces focused kind validation to a smaller but still multi-node NiFi shape.
  - Sets `replicaCount: 2`, lowers heap and pod resources, shrinks PVC sizes, and disables the PDB for focused reruns.
  - Compose it with focused kind overlays only. Do not use it as a replacement for the proven baseline profiles or `make kind-alpha-e2e`.

- [platform-fast-values.yaml](platform-fast-values.yaml)
  - Product-chart equivalent of the focused fast overlay.
  - Nests the same smaller multi-node shape under `nifi.*` for `charts/nifi-platform`.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml) or [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml).
  - The primary focused runtime commands are `make kind-platform-managed-fast-e2e` and `make kind-platform-managed-cert-manager-fast-e2e`.

Metrics note:

- [platform-managed-metrics-native-values.yaml](platform-managed-metrics-native-values.yaml) is an optional overlay for the first-class native API metrics subsystem
- it enables `nifi.observability.metrics.mode=nativeApi`
- it is the recommended default metrics overlay for managed installs
- it renders a dedicated metrics `Service` plus multiple named `ServiceMonitor` resources
- it uses the provider-agnostic machine-auth Secret and CA Secret contract shared by the metrics subsystem
- `hack/bootstrap-metrics-machine-auth.sh` can create those Kubernetes Secrets from a pre-minted token or from existing NiFi-accepted credentials
- the focused live runtime proof command is `make kind-metrics-native-api-fast-e2e`
- the broader focused matrix command is `make kind-metrics-fast-e2e`
- the current live proof covers the secured flow-metrics endpoint and two named scrape profiles against that same endpoint
- [platform-managed-trust-manager-values.yaml](platform-managed-trust-manager-values.yaml) is an optional overlay for trust-manager-based shared CA bundle distribution
- it enables `trustManager.enabled=true`
- it enables `trustManager.mirrorTLSSecret.enabled=true` so the workload TLS `ca.crt` is mirrored into trust-manager's trust namespace automatically
- it wires the resulting bundle into optional NiFi extra trust import
- nativeApi and exporter can also consume the same bundle through `*.tlsConfig.ca.useTrustManagerBundle=true`
- the focused runtime proof command is `make kind-platform-managed-trust-manager-fast-e2e`
- [platform-managed-metrics-native-trust-manager-values.yaml](platform-managed-metrics-native-trust-manager-values.yaml) layers trust-manager-backed native API metrics on top of the managed metrics overlay
- it switches the Bundle target to a Secret, enables an additional PKCS12 output, and points nativeApi TLS trust at the trust-manager bundle
- use it together with `examples/platform-managed-values.yaml`, `examples/platform-managed-trust-manager-values.yaml`, and `examples/platform-managed-metrics-native-values.yaml`
- the focused runtime proof command is `make kind-metrics-native-api-trust-manager-fast-e2e`
- [platform-managed-metrics-exporter-values.yaml](platform-managed-metrics-exporter-values.yaml) is an optional overlay for the supported exporter metrics mode
- it enables `nifi.observability.metrics.mode=exporter`
- it renders a small companion exporter `Deployment`, a clean HTTP metrics `Service`, and one exporter `ServiceMonitor`
- it uses the same provider-agnostic machine-auth Secret and CA Secret contract
- the focused live runtime proof command is `make kind-metrics-exporter-fast-e2e`
- the broader focused matrix command is `make kind-metrics-fast-e2e`
- the current live proof covers the secured `/nifi-api/flow/metrics/prometheus` endpoint republished on exporter `/metrics`
- it also enables selected controller-status gauges derived from `/nifi-api/flow/status`
- the live proof also covers upstream-aware readiness and mounted auth Secret rotation without restarting the exporter pod
- [platform-managed-metrics-site-to-site-values.yaml](platform-managed-metrics-site-to-site-values.yaml) is an optional prepared-only overlay for a future site-to-site metrics path
- it enables `nifi.observability.metrics.mode=siteToSite`
- it documents the intended destination, auth, TLS, source, transport, and format contract for a future `SiteToSiteMetricsReportingTask` integration
- it validates that contract at Helm render time and then still fails clearly because runtime ownership is not implemented
- it is intentionally excluded from the live metrics runtime proof matrix

KEDA note:

- [platform-managed-keda-values.yaml](platform-managed-keda-values.yaml) is an optional experimental overlay for KEDA-triggered external scale-up intent in managed mode
- [platform-managed-keda-scale-down-values.yaml](platform-managed-keda-scale-down-values.yaml) adds opt-in experimental controller-mediated external downscale intent on top of the managed KEDA overlay
- use it only with `charts/nifi-platform`, for example: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-values.yaml -f examples/platform-managed-keda-values.yaml`
- add `-f examples/platform-managed-keda-scale-down-values.yaml` only when you want KEDA to write best-effort lower `/scale` intent for the controller to evaluate
- it renders a `ScaledObject` that targets `NiFiCluster`, not the NiFi `StatefulSet`
- it does not add any KEDA resources or values to `charts/nifi`
- the controller still performs all actual scale-up and scale-down execution
- the focused live runtime proof commands are `make kind-keda-scale-up-fast-e2e` and `make kind-keda-scale-down-fast-e2e`
- see [../docs/keda.md](../docs/keda.md) for the current recommendation and ownership model

There are also prepared authentication overlays:

- [oidc-values.yaml](oidc-values.yaml)
  - Enables `auth.mode=oidc`.
  - Compose with [managed/values.yaml](managed/values.yaml).
  - Pair with [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml) for NiFi application groups, policies, and external proxy hosts.
  - Use [oidc-kind-values.yaml](oidc-kind-values.yaml) for the focused kind OIDC evaluator.

- [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml)
  - Seeds NiFi application groups and file-managed policies for OIDC group claims.
  - Group names in the token must match these NiFi application group names exactly.
  - The current chart now renders the richer policy file in a NiFi 2-compatible order instead of crashing at startup.
  - End-to-end browser-flow authorization proof for observer, operator, and admin groups is still conservative on the current local Keycloak `26.x` path.

- [oidc-kind-values.yaml](oidc-kind-values.yaml)
  - Focused kind OIDC overlay.
  - Keeps the flow internal to the cluster.
  - Uses the documented `Initial Admin Identity` fallback for the first admin path.
  - The focused runtime commands are `make kind-auth-oidc-e2e` and `make kind-auth-oidc-nifi-2-8-fast-e2e` when composed with [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml) and [test-fast-values.yaml](test-fast-values.yaml).
  - Treat the current kind evaluator as an active hardening path for browser-flow proof, not as a blanket claim that every local Keycloak combination is green.

- [oidc-external-url-values.yaml](oidc-external-url-values.yaml)
  - Adds an ingress-backed public HTTPS host and matching `web.proxyHosts` entry for OIDC redirects.
  - Compose with [oidc-values.yaml](oidc-values.yaml) and [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml).
  - Current local ingress-backed OIDC runtime proof is still conservative while the focused browser-flow evaluator is being hardened.

- [ldap-values.yaml](ldap-values.yaml)
  - Enables `auth.mode=ldap` with `authz.mode=ldapSync`.
  - Use [ldap-kind-values.yaml](ldap-kind-values.yaml) for the focused kind LDAP evaluator.

- [ldap-kind-values.yaml](ldap-kind-values.yaml)
  - Focused kind LDAP overlay.
  - Uses the documented `Initial Admin Identity` bootstrap path.
  - The focused runtime command is `make kind-auth-ldap-e2e`.

- [ingress-proxy-host-values.yaml](ingress-proxy-host-values.yaml)
  - Generic ingress and `web.proxyHosts` overlay for auth-enabled browser access.
  - Prepared only. Adjust hostnames, ingress class, and annotations for your environment.

- [openshift/route-proxy-host-values.yaml](openshift/route-proxy-host-values.yaml)
  - OpenShift passthrough Route host plus matching `web.proxyHosts`.
  - Compose with OpenShift overlays and either OIDC or LDAP when the cluster is available.
  - Render and docs only in this slice. No real OpenShift runtime proof is claimed here.

There are also prepared Flow Registry Client overlays:

- [github-flow-registry-values.yaml](github-flow-registry-values.yaml)
  - Prepared GitHub Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [github-flow-registry-kind-values.yaml](github-flow-registry-kind-values.yaml)
  - Focused kind GitHub Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml), [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml), and [test-fast-values.yaml](test-fast-values.yaml).
  - The focused runtime commands are `make kind-flow-registry-github-fast-e2e` and `make kind-flow-registry-github-fast-e2e-reuse`.

- [gitlab-flow-registry-values.yaml](gitlab-flow-registry-values.yaml)
  - Prepared GitLab Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [gitlab-flow-registry-kind-values.yaml](gitlab-flow-registry-kind-values.yaml)
  - Focused kind GitLab Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml), [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml), and optionally [test-fast-values.yaml](test-fast-values.yaml).
  - The focused runtime command is `make kind-flow-registry-gitlab-e2e`.
  - The focused rerun command is `KIND_CLUSTER_NAME=nifi-fabric-flow-registry-gitlab make kind-flow-registry-gitlab-e2e-reuse`.

- [bitbucket-flow-registry-values.yaml](bitbucket-flow-registry-values.yaml)
  - Prepared Bitbucket Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

- [bitbucket-flow-registry-kind-values.yaml](bitbucket-flow-registry-kind-values.yaml)
  - Focused kind Bitbucket Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml), [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml), and [test-fast-values.yaml](test-fast-values.yaml).
  - The focused runtime commands are `make kind-flow-registry-bitbucket-fast-e2e` and `make kind-flow-registry-bitbucket-fast-e2e-reuse`.

- [azure-devops-flow-registry-values.yaml](azure-devops-flow-registry-values.yaml)
  - Prepared Azure DevOps Flow Registry Client catalog entry.
  - Renders a validated definition only; it does not auto-create the client in NiFi.

There is also one focused NiFi version compatibility overlay:

- [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml)
  - Overrides the chart image tag to `apache/nifi:2.8.0`.
  - Uses `replicaCount: 2` for the focused multi-node kind compatibility proof.
  - Compose with either [standalone/values.yaml](standalone/values.yaml) or [managed/values.yaml](managed/values.yaml).
  - It also composes with the existing OIDC overlays for the focused `apache/nifi:2.8.0` OIDC proof path.
  - The focused managed proof commands are `make kind-nifi-2-8-e2e` and `make kind-nifi-2-8-fast-e2e`.

Only one authentication mode is supported at a time. The intended thin-platform combinations are:

- `singleUser + fileManaged`
- `oidc + externalClaimGroups`
- `ldap + ldapSync`

Preferred bootstrap:

- `authz.bootstrap.initialAdminGroup`

Fallback bootstrap:

- `authz.bootstrap.initialAdminIdentity`

Focused auth evaluator commands:

- `make kind-auth-oidc-e2e`
- `make kind-auth-oidc-nifi-2-8-fast-e2e`
- `make kind-auth-ldap-e2e`
- `make kind-nifi-2-8-e2e`
- `make kind-flow-registry-gitlab-e2e`
- `make kind-flow-registry-github-fast-e2e`
- `make kind-flow-registry-bitbucket-fast-e2e`
- `make kind-auth-oidc-fast-e2e`
- `make kind-auth-ldap-fast-e2e`
- `make kind-nifi-2-8-fast-e2e`
- `make kind-flow-registry-gitlab-fast-e2e`

Flow Registry Client notes:

- classic NiFi Registry is not the preferred direction here
- Git-based Flow Registry Clients are preferred
- the chart renders a prepared catalog under `flowRegistryClients.mountPath`
- the catalog is available as both `clients.yaml` and `clients.json`
- there is no controller-managed flow import or synchronization
- the focused kind proof covers the GitLab client path on NiFi `2.8.0` against a GitLab-compatible evaluator service
- the focused kind proof also covers the GitHub client path on NiFi `2.8.0` against a GitHub-compatible evaluator service with the fast profile
- the focused kind proof also covers the Bitbucket client path on NiFi `2.8.0` against a Bitbucket-compatible evaluator service with the fast profile

## Standalone

- [platform-standalone-values.yaml](platform-standalone-values.yaml)
  - Minimal one-release product-chart values for standalone mode.
  - Use with `charts/nifi-platform`.
  - The reusable app chart still comes from `charts/nifi`.

- [standalone/values.yaml](standalone/values.yaml)
  - Minimal app-chart values for a standalone NiFi 2 install on kind.
  - Use with `make helm-install-standalone`.

## Managed

- [platform-managed-values.yaml](platform-managed-values.yaml)
  - Minimal one-release product-chart values for managed mode.
  - Installs the CRD, controller, RBAC, app chart, and `NiFiCluster` in one Helm release.
  - Requires the controller image to be reachable by the target cluster.
  - The primary focused runtime proof commands are `make kind-platform-managed-fast-e2e` and `make kind-platform-managed-fast-e2e-reuse`.

- [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml)
  - Minimal one-release product-chart values for managed mode when cert-manager already exists in the cluster.
  - cert-manager remains a prerequisite and is not bundled by this chart.
  - Requires the stable `nifi-tls-params` Secret for the PKCS12 password and `nifi.sensitive.props.key`.
  - The primary focused runtime proof commands are `make kind-platform-managed-cert-manager-fast-e2e` and `make kind-platform-managed-cert-manager-fast-e2e-reuse`.

- [managed/values.yaml](managed/values.yaml)
  - Minimal app-chart values for managed mode.
  - Use with `make helm-install-managed`.

- [managed/nificluster.yaml](managed/nificluster.yaml)
  - Minimal `NiFiCluster` for advanced manual managed assembly in the `Running` state.
  - Use with `make apply-managed`.

## Rollout Trigger

- [managed/rollout-trigger-values.yaml](managed/rollout-trigger-values.yaml)
  - Minimal Helm values overlay that changes a pod template annotation.
  - Use to trigger the managed `OnDelete` revision rollout path.

## Hibernate And Restore

- [managed/nificluster-hibernated.yaml](managed/nificluster-hibernated.yaml)
  - Minimal `NiFiCluster` example for the `Hibernated` state.
  - Apply it to hibernate the managed cluster.
  - Restore with [managed/nificluster.yaml](managed/nificluster.yaml).
