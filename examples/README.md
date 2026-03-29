# Examples

These examples cover both the product-facing platform chart and the lower-level app-chart or evaluator overlays.

## Start Here

For most teams, start with one of these `charts/nifi-platform` entry points:

- [platform-managed-cert-manager-quickstart-values.yaml](platform-managed-cert-manager-quickstart-values.yaml)
  - Standard cert-manager-first quickstart.
  - Recommended fastest first install once cert-manager and the target `Issuer` or `ClusterIssuer` already exist.
  - Bootstraps the initial single-user login and TLS inputs, while cert-manager creates the final workload TLS Secret.

- [platform-managed-quickstart-values.yaml](platform-managed-quickstart-values.yaml)
  - Secondary self-signed quickstart.
  - Use it for evaluation when you do not want the cert-manager prerequisite.
  - This is not the default recommended customer path.

- [platform-managed-values.yaml](platform-managed-values.yaml)
  - Explicit advanced path using externally provided TLS and auth inputs.
  - Use it when you want to own those inputs from the start.

- [platform-managed-configmap-properties-values.yaml](platform-managed-configmap-properties-values.yaml)
  - Optional overlay for ordered external `nifi.properties` overrides from ConfigMaps.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml).
  - Create the referenced ConfigMaps before install, or use the dedicated kind ConfigMap-properties runtime proof script.

- [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml)
  - Explicit advanced cert-manager path.
  - Use it when cert-manager already exists and you want explicit ownership of the remaining bootstrap inputs instead of the quickstart flow.

The quickstart paths are valid for evaluation and low-friction first installs, but they are not a substitute for explicit enterprise auth planning.

Primary one-command product installs:

- managed standard: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace --set global.nifiFabric.installProfile=quickstart-cert-manager --set nifi.tls.certManager.issuerRef.name=nifi-ca`
- managed advanced explicit external-secret: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-values.yaml`
- managed advanced explicit cert-manager: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-cert-manager-values.yaml`
- managed self-signed quickstart: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace --set global.nifiFabric.installProfile=quickstart-self-signed`
- standalone: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-standalone-values.yaml`

Generated manifest-bundle installs:

- managed: `make render-platform-managed-bundle && kubectl apply -f dist/nifi-platform-managed-bundle.yaml`
- managed + cert-manager: `make render-platform-managed-cert-manager-bundle && kubectl apply -f dist/nifi-platform-managed-cert-manager-bundle.yaml`
- fresh packaged chart for downstreams: `make package-platform-chart`

Advanced evaluator installs still exist:

- standalone: `make install-standalone`
- managed: `make install-managed`
- managed + cert-manager: `make install-managed-cert-manager`

There is also one AKS overlay set:

- [aks/standalone-values.yaml](aks/standalone-values.yaml)
  - AKS starting point for standalone installs.
  - Use it when you want the lower-level `charts/nifi` shape on AKS.

- [aks/managed-values.yaml](aks/managed-values.yaml)
  - AKS starting point for managed-mode installs.
  - Compose with [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml) if cert-manager already exists in the AKS cluster.
  - Aligns with the supported AKS managed install direction.

There is also one OpenShift overlay set:

- [openshift/standalone-values.yaml](openshift/standalone-values.yaml)
  - OpenShift standalone overlay for `charts/nifi`.
  - Compose with [standalone/values.yaml](standalone/values.yaml).
  - Keeps the Service internal first and leaves Route enablement to the separate Route overlay.
  - Use it when you want the lower-level `charts/nifi` shape on OpenShift.

- [openshift/managed-values.yaml](openshift/managed-values.yaml)
  - OpenShift overlay for the standard `charts/nifi-platform` managed install path.
  - Compose with [platform-managed-values.yaml](platform-managed-values.yaml).
  - Keeps the Service internal, relaxes fixed UID or GID settings for both the controller and NiFi workload, and keeps external Route exposure on the separate explicit host overlay.
  - Compose with [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml) when cert-manager already exists in the cluster.

There is also one optional TLS-source overlay:

- [cert-manager-values.yaml](cert-manager-values.yaml)
  - Switches the chart from `tls.mode=externalSecret` to `tls.mode=certManager`.
  - Use it on top of either the standalone or managed Helm values when cert-manager and the `nifi-ca` issuer bootstrap are already installed.
  - Still requires a separate Secret for the PKCS12 password and `nifi.sensitive.props.key`.
  - For kind evaluator setup, run `make kind-bootstrap-cert-manager` first.

There is also one optional fast overlay:

- [test-fast-values.yaml](test-fast-values.yaml)
  - Reduces kind installs to a smaller but still multi-node NiFi shape.
  - Sets `replicaCount: 2`, lowers heap and pod resources, shrinks PVC sizes, and disables the PDB for faster reruns.
  - Compose it with kind overlays only. Do not use it as a replacement for the baseline profiles or `make kind-alpha-e2e`.

- [platform-fast-values.yaml](platform-fast-values.yaml)
  - Product-chart equivalent of the fast overlay.
  - Nests the same smaller multi-node shape under `nifi.*` for `charts/nifi-platform`.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml) or [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml).

- [platform-managed-restore-kind-values.yaml](platform-managed-restore-kind-values.yaml)
  - Product-chart overlay for the restore workflow.
  - Enables a kind-local GitHub Flow Registry Client catalog entry, one runtime-managed Parameter Context, and one versioned-flow import selection.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml) and [platform-fast-values.yaml](platform-fast-values.yaml).

Recovery note:

- for control-plane recovery planning, export the live release intent with `bash hack/export-control-plane-backup.sh --release nifi --namespace nifi --output-dir ./backup/nifi-control-plane`
- recover that declarative layer later with `bash hack/recover-control-plane-backup.sh --backup-dir ./backup/nifi-control-plane`
- those helpers rebuild the declarative control plane only; pair them with your normal Secret escrow and PVC snapshot recovery procedures
- see [../docs/dr.md](../docs/dr.md) for the full recovery model and boundaries

- [platform-managed-linkerd-values.yaml](platform-managed-linkerd-values.yaml)
  - Optional Linkerd compatibility overlay for the product chart.
  - Injects only the NiFi StatefulSet pods and keeps the controller outside the mesh.
  - Marks the NiFi cluster protocol and load-balance ports opaque in the documented baseline profile.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml).

- [platform-managed-istio-values.yaml](platform-managed-istio-values.yaml)
  - Optional Istio sidecar-mode compatibility overlay for the product chart.
  - Injects only the NiFi StatefulSet pods and keeps the controller outside the mesh.
  - Enables the documented sidecar-mode annotations for probe rewrite and waiting for the sidecar before NiFi starts.
  - The supported profile still expects the operator to enable Istio sidecar injection on the NiFi namespace only.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml).

- [platform-managed-istio-ambient-values.yaml](platform-managed-istio-ambient-values.yaml)
  - Optional Istio Ambient compatibility overlay for the product chart.
  - Enrolls only the NiFi StatefulSet pods and keeps the controller outside Ambient.
  - Uses pod-template labels only, with no sidecars and no waypoint behavior in the supported profile.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml).

Metrics note:

- [platform-managed-metrics-native-values.yaml](platform-managed-metrics-native-values.yaml) is an optional overlay for the first-class native API metrics subsystem
- it enables `nifi.observability.metrics.nativeApi.serviceMonitor.enabled=true`
- the platform chart already defaults managed installs to `nifi.observability.metrics.mode=nativeApi` and keeps `ServiceMonitor` opt-in until Prometheus Operator is present
- it renders a dedicated metrics `Service` plus multiple named `ServiceMonitor` resources
- it uses the provider-agnostic machine-auth Secret and CA Secret layout shared by the metrics subsystem
- `hack/bootstrap-metrics-machine-auth.sh` can create those Kubernetes Secrets from a pre-minted token or from existing NiFi-accepted credentials
- local kind coverage includes the secured flow-metrics endpoint and two named scrape profiles against that same endpoint
- [platform-managed-trust-manager-values.yaml](platform-managed-trust-manager-values.yaml) is an optional overlay for trust-manager-based shared CA bundle distribution
- it enables `trustManager.enabled=true`
- it enables `trustManager.mirrorTLSSecret.enabled=true` so the workload TLS `ca.crt` is mirrored into trust-manager's trust namespace automatically
- it wires the resulting bundle into optional NiFi extra trust import
- nativeApi and exporter can also consume the same bundle through `*.tlsConfig.ca.useTrustManagerBundle=true`
- [platform-managed-metrics-native-trust-manager-values.yaml](platform-managed-metrics-native-trust-manager-values.yaml) layers trust-manager-backed native API metrics on top of the managed metrics overlay
- it switches the Bundle target to a Secret, enables an additional PKCS12 output, and points nativeApi TLS trust at the trust-manager bundle
- use it together with `examples/platform-managed-values.yaml`, `examples/platform-managed-trust-manager-values.yaml`, and `examples/platform-managed-metrics-native-values.yaml`
- [platform-managed-metrics-exporter-values.yaml](platform-managed-metrics-exporter-values.yaml) is an optional overlay for the supported exporter metrics mode
- it enables `nifi.observability.metrics.mode=exporter`
- it renders a small companion exporter `Deployment`, a clean HTTP metrics `Service`, and one exporter `ServiceMonitor`
- it uses the same provider-agnostic machine-auth Secret and CA Secret layout
- nativeApi remains the primary recommended metrics path; use this overlay only when you specifically want the exporter shape
- local kind coverage includes the secured `/nifi-api/flow/metrics/prometheus` endpoint with the upstream NiFi metric-family set preserved on exporter `/metrics`
- it also enables controller-status gauges derived from `/nifi-api/flow/status`
- local kind coverage also includes upstream-aware readiness and mounted auth Secret rotation without restarting the exporter pod
- [platform-managed-metrics-exporter-trust-manager-values.yaml](platform-managed-metrics-exporter-trust-manager-values.yaml) layers trust-manager-backed exporter upstream trust on top of the managed exporter overlay
- it switches the Bundle target to a Secret and points exporter source TLS trust at the trust-manager bundle instead of a manually created CA Secret
- use it together with `examples/platform-managed-values.yaml`, `examples/platform-managed-trust-manager-values.yaml`, and `examples/platform-managed-metrics-exporter-values.yaml`
- [platform-managed-metrics-site-to-site-values.yaml](platform-managed-metrics-site-to-site-values.yaml) is an optional overlay for the GA sender-side typed site-to-site metrics export path
- it enables `nifi.observability.metrics.mode=siteToSite`
- it enables `nifi.observability.metrics.siteToSite.enabled=true`
- it models the destination, auth, receiver-authorized identity, source, transport, and format settings for one `SiteToSiteMetricsReportingTask`
- it keeps destination receiver topology and destination-side user or policy lifecycle operator-owned
- [platform-managed-metrics-site-to-site-kind-values.yaml](platform-managed-metrics-site-to-site-kind-values.yaml) points that typed feature at a cluster-local kind URL for local kind use
- [standalone-site-to-site-receiver-kind-values.yaml](standalone-site-to-site-receiver-kind-values.yaml) is the kind receiver harness used by that command
- the harness bootstraps one public input port, one minimal downstream processor, and the minimum receiver-side auth needed to trust and authorize the declared sender identity for delivery
- site-to-site status is its own optional GA sender-side typed path and is not part of the `observability.metrics.mode=siteToSite` metrics claim
- [platform-managed-site-to-site-status-values.yaml](platform-managed-site-to-site-status-values.yaml) is an optional overlay for the GA sender-side typed site-to-site status export path
- it enables `nifi.observability.siteToSiteStatus.enabled=true`
- it models the destination, auth, receiver-authorized identity, optional source instance URL override, and transport settings for one `SiteToSiteStatusReportingTask`
- it keeps JSON status payload shape, platform, batching, and filters fixed behind the typed API
- it keeps destination receiver topology and destination-side user or policy lifecycle operator-owned
- [platform-managed-site-to-site-status-kind-values.yaml](platform-managed-site-to-site-status-kind-values.yaml) points that typed feature at a cluster-local kind URL for local kind use
- [standalone-site-to-site-receiver-kind-values.yaml](standalone-site-to-site-receiver-kind-values.yaml) is reused as the kind receiver harness for that command
- site-to-site provenance is its own optional GA sender-side typed path and is not part of the `observability.metrics.mode=siteToSite` metrics claim
- [platform-managed-site-to-site-provenance-values.yaml](platform-managed-site-to-site-provenance-values.yaml) is an optional overlay for the GA sender-side typed site-to-site provenance export path
- it enables `nifi.observability.siteToSiteProvenance.enabled=true`
- it models the destination, auth, receiver-authorized identity, optional source instance URL override, transport settings, and a small provenance cursor for one `SiteToSiteProvenanceReportingTask`
- it keeps fixed platform, batching, and schedule defaults behind the typed API
- it keeps destination receiver topology, destination-side user or policy lifecycle, long-lived credential lifecycle, downstream provenance processing, and downstream storage or retention expectations operator-owned
- [platform-managed-site-to-site-provenance-kind-values.yaml](platform-managed-site-to-site-provenance-kind-values.yaml) points that typed feature at a cluster-local kind URL for local kind use
- [standalone-site-to-site-receiver-kind-values.yaml](standalone-site-to-site-receiver-kind-values.yaml) is reused as the kind receiver harness for that command

Audit note:

- [platform-managed-audit-flow-actions-local-only-values.yaml](platform-managed-audit-flow-actions-local-only-values.yaml) is the local-only example for enabling durable NiFi-native audit while keeping external export disabled
- [platform-managed-audit-flow-actions-ghcr-values.yaml](platform-managed-audit-flow-actions-ghcr-values.yaml) is the connected-cluster example for using a published reporter image directly
- [platform-managed-audit-flow-actions-private-registry-values.yaml](platform-managed-audit-flow-actions-private-registry-values.yaml) is the restricted-cluster example for using a mirrored internal reporter image plus `imagePullSecrets`
- [platform-managed-audit-flow-actions-values.yaml](platform-managed-audit-flow-actions-values.yaml) is the optional managed-platform overlay for bounded design-time flow-action audit export
- it enables `nifi.observability.audit.flowActions.enabled=true`
- it keeps durable local history, request log, and flow archive as the primary support layer
- it adds only the advanced `export.type=log` path; HTTP and Kafka sinks are intentionally not part of this shape
- it expects a reporter image containing the NAR at the configured path; build the local example image with `make build-flow-action-audit-reporter-image`, or use the release-published upstream image from `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter`
- it pins the NiFi workload image to `2.8.0` because FlowActionReporter is only available in published NiFi artifacts from `2.4.0` onward
- use it together with `examples/platform-managed-values.yaml`
- [platform-managed-audit-flow-actions-kind-values.yaml](platform-managed-audit-flow-actions-kind-values.yaml) is the focused local kind overlay for this proof path
- it switches the proof to a single NiFi node and keeps startup on the normal path until the proof script grants one temporary root-canvas policy through the NiFi API
- use it together with `examples/platform-managed-values.yaml`, one audit overlay, and `examples/platform-fast-values.yaml`
- use `make kind-flow-action-audit-fast-e2e` for the focused local kind proof path that installs local-only audit first and then upgrades to `export.type=log`
- production rollout starts with the local-only overlay above, then moves to one of the explicit reporter-image overlays once the reporter image source and pull path are validated in the target cluster

Log shipping note:

- [platform-managed-log-shipping-vector-values.yaml](platform-managed-log-shipping-vector-values.yaml) is the optional managed-platform overlay for one documented sidecar-based log-shipping pattern
- [log-shipping-vector-configmap.yaml](log-shipping-vector-configmap.yaml) is the matching sample Vector ConfigMap
- use them together with `examples/platform-managed-values.yaml`
- the example mounts the existing NiFi `logs` volume plus a writable Vector state directory into one sidecar and writes structured events to the sidecar stdout stream for cluster log collection
- this is intentionally a docs-first sidecar pattern, not a built-in logging subsystem
- see [../docs/operations/log-shipping.md](../docs/operations/log-shipping.md) for the operator guidance and tradeoffs

KEDA note:

- [platform-managed-keda-values.yaml](platform-managed-keda-values.yaml) is the optional GA overlay for KEDA-triggered external scale-up intent in managed mode
- [platform-managed-keda-scale-down-values.yaml](platform-managed-keda-scale-down-values.yaml) adds the GA controller-mediated external downscale path on top of the managed KEDA overlay
- use it only with `charts/nifi-platform`, for example: `helm upgrade --install nifi charts/nifi-platform -n nifi --create-namespace -f examples/platform-managed-values.yaml -f examples/platform-managed-keda-values.yaml`
- add `-f examples/platform-managed-keda-scale-down-values.yaml` only when you want KEDA to write best-effort lower `/scale` intent for the controller to evaluate
- it renders a `ScaledObject` that targets `NiFiCluster`, not the NiFi `StatefulSet`
- the overlay intentionally leaves `cluster.autoscaling.external.requestedReplicas` at its runtime-managed default of `0`; KEDA updates that field later through `/scale`
- it does not add any KEDA resources or values to `charts/nifi`
- the controller still performs all actual scale-up and scale-down execution
- the controller now reports the raw KEDA request, controller-evaluated intent, and blocked, ignored, or deferred handling through `status.autoscaling.external.*`
- see [../docs/keda.md](../docs/keda.md) for the current recommendation and ownership model

There are also authentication overlays:

- [oidc-values.yaml](oidc-values.yaml)
  - Enables `auth.mode=oidc`.
  - Compose with [managed/values.yaml](managed/values.yaml).
  - Pair with [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml) for NiFi application groups, policies, and external proxy hosts.

- [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml)
  - Seeds NiFi application groups and file-managed policies for OIDC group claims.
  - Group names in the token must match these NiFi application group names exactly.
  - The current chart now renders the richer policy file in a NiFi 2-compatible order instead of crashing at startup.
  - Use it on the `oidc + externalClaimGroups` model when you want external group claims mapped to named NiFi access bundles.

- [mutable-flow-authz-values.yaml](mutable-flow-authz-values.yaml)
  - Enables the mutable-flow capability bundle for chart-managed groups.
  - Seeds the root canvas policies needed for process-group editing and version-control actions.
  - Compose it with [managed/values.yaml](managed/values.yaml) or with the OIDC group-claims overlays when those external groups should be allowed to edit flows.

- [authz-policy-bundles-values.yaml](authz-policy-bundles-values.yaml)
  - Enables the recommended named policy bundles for viewer, editor, and version-manager group bindings.
  - Compose it with [managed/values.yaml](managed/values.yaml).

- [oidc-kind-values.yaml](oidc-kind-values.yaml)
  - Kind OIDC overlay.
  - Keeps the flow internal to the cluster.
  - Uses the documented `Initial Admin Identity` fallback for the first admin path.

- [oidc-kind-initial-admin-group-values.yaml](oidc-kind-initial-admin-group-values.yaml)
  - Kind OIDC overlay for using `authz.bootstrap.initialAdminGroup` as the primary bootstrap path.
  - Keeps the flow internal to the cluster while leaving the seeded admin group as the first-admin route.

- [oidc-external-url-values.yaml](oidc-external-url-values.yaml)
  - Adds an ingress-backed public HTTPS host and matching `web.proxyHosts` entry for OIDC redirects.
  - Compose with [oidc-values.yaml](oidc-values.yaml) and [oidc-group-claims-values.yaml](oidc-group-claims-values.yaml).
  - Use it when the external browser flow stays on the same `oidc + externalClaimGroups` model and NiFi needs a public HTTPS host for redirects.

- [values-dev-keycloak-bootstrap.yaml](values-dev-keycloak-bootstrap.yaml)
  - Development OIDC overlay for `charts/nifi-platform`.
  - Convenience path for local, personal, and demo environments.
  - Pair it with [platform-managed-values.yaml](platform-managed-values.yaml) and [keycloak-dev-bootstrap-realm.json](keycloak-dev-bootstrap-realm.json).

- [keycloak-dev-bootstrap-realm.json](keycloak-dev-bootstrap-realm.json)
  - Sample Keycloak startup-import realm for the dev bootstrap OIDC path.
  - Seeds a client, groups, and an optional local dev admin user.
  - Non-production example only.

- [values-prod-oidc.yaml](values-prod-oidc.yaml)
  - Production OIDC overlay for `charts/nifi-platform`.
  - Recommended customer path once the customer-managed Keycloak realm, groups, users, and client already exist.
  - Pair it with [../docs/install/keycloak-oidc-production.md](../docs/install/keycloak-oidc-production.md) for the production setup steps.

- [integrated-keycloak-oidc-contract.yaml](integrated-keycloak-oidc-contract.yaml)
  - Advanced higher-level install contract sketch for bootstrapping Keycloak and NiFi-Fabric together.
  - Not consumed directly by `charts/nifi-platform`.
  - Shows the recommended “declare the OIDC client secret once and use it in both Keycloak and `Secret/nifi-oidc`” pattern.

- [ldap-values.yaml](ldap-values.yaml)
  - Enables `auth.mode=ldap` with `authz.mode=ldapSync`.
  - Pair it with [../docs/install/ldap-production.md](../docs/install/ldap-production.md) for the production setup steps and baseline identity-bootstrap path.

- [ldap-group-bootstrap-values.yaml](ldap-group-bootstrap-values.yaml)
  - Enables LDAP group bootstrap for newer NiFi images.
  - Compose it with [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml) and [../docs/install/ldap-production.md](../docs/install/ldap-production.md).

- [ldap-kind-values.yaml](ldap-kind-values.yaml)
  - Kind LDAP overlay.
  - Uses the documented `Initial Admin Identity` bootstrap path.

- [ingress-proxy-host-values.yaml](ingress-proxy-host-values.yaml)
  - Generic ingress and `web.proxyHosts` overlay for auth-enabled browser access.
  - Adjust hostnames, ingress class, and annotations for your environment.

- [openshift/route-proxy-host-values.yaml](openshift/route-proxy-host-values.yaml)
  - OpenShift passthrough Route host plus matching `web.proxyHosts`.
  - Compose with the OpenShift managed or standalone overlays when you need native OpenShift external HTTPS access.
  - The supported shape is an explicit Route host plus matching NiFi proxy host on a passthrough Route.

There are also Flow Registry Client overlays:

- [github-flow-registry-values.yaml](github-flow-registry-values.yaml)
  - GitHub Flow Registry Client catalog entry.
  - Renders a catalog definition only; it does not auto-create the client in NiFi.

- [github-flow-registry-kind-values.yaml](github-flow-registry-kind-values.yaml)
  - Kind GitHub Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml) and a suitable local kind overlay.

- [github-flow-registry-workflow-values.yaml](github-flow-registry-workflow-values.yaml)
  - GitHub versioned-flow workflow overlay.
  - Adds the `flowVersionManager` authz bundle and single-node shape used for the save-to-registry flow.
  - Compose with [managed/values.yaml](managed/values.yaml), [github-flow-registry-kind-values.yaml](github-flow-registry-kind-values.yaml), and a suitable local kind overlay.

- [gitlab-flow-registry-values.yaml](gitlab-flow-registry-values.yaml)
  - GitLab Flow Registry Client catalog entry.
  - Renders a catalog definition only; it does not auto-create the client in NiFi.

- [gitlab-flow-registry-kind-values.yaml](gitlab-flow-registry-kind-values.yaml)
  - Kind GitLab Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml) and a suitable local kind overlay.

- [bitbucket-flow-registry-values.yaml](bitbucket-flow-registry-values.yaml)
  - Bitbucket Flow Registry Client catalog entry.
  - Renders a catalog definition only; it does not auto-create the client in NiFi.

- [bitbucket-flow-registry-kind-values.yaml](bitbucket-flow-registry-kind-values.yaml)
  - Kind Bitbucket Flow Registry Client runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml) and a suitable local kind overlay.

- [nifi-registry-flow-registry-values.yaml](nifi-registry-flow-registry-values.yaml)
  - NiFi Registry compatibility Flow Registry Client catalog entry.
  - Compose with standalone or managed values when you want the typed NiFi Registry client definition rendered into the pod-mounted catalog.

- [nifi-registry-flow-registry-kind-values.yaml](nifi-registry-flow-registry-kind-values.yaml)
  - Kind NiFi Registry compatibility runtime overlay.
  - Compose with [managed/values.yaml](managed/values.yaml) and a suitable local kind overlay.
  - Uses a real in-cluster `apache/nifi-registry` deployment and checks client creation against live NiFi runtime APIs.

- [azure-devops-flow-registry-values.yaml](azure-devops-flow-registry-values.yaml)
  - Azure DevOps Flow Registry Client catalog entry.
  - Renders a catalog definition only; it does not auto-create the client in NiFi.

There are also Parameter Context overlays:

- [platform-managed-parameter-contexts-values.yaml](platform-managed-parameter-contexts-values.yaml)
  - Runtime-managed Parameter Context entry for the standard `charts/nifi-platform` path, including one direct root-child attachment target.
  - It models one context with inline non-sensitive values, a sensitive Kubernetes Secret reference, and one external Parameter Provider reference.
  - It creates or updates that declared context in NiFi through the pod bootstrap path.
  - It does not create Parameter Providers or assign contexts to process groups.

- [platform-managed-parameter-contexts-kind-values.yaml](platform-managed-parameter-contexts-kind-values.yaml)
  - Kind overlay for runtime-managed Parameter Context checks.
  - It also enables the mutable-flow bootstrap permission used only to seed the example root-child process group.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml), [platform-fast-values.yaml](platform-fast-values.yaml), and [platform-managed-parameter-contexts-values.yaml](platform-managed-parameter-contexts-values.yaml).

- [platform-managed-parameter-contexts-update-kind-values.yaml](platform-managed-parameter-contexts-update-kind-values.yaml)
  - Update overlay used to demonstrate reconcile behavior after a restart.

There are also versioned-flow import overlays:

- [platform-managed-versioned-flow-import-values.yaml](platform-managed-versioned-flow-import-values.yaml)
  - Runtime-managed versioned-flow import for the standard `charts/nifi-platform` path.
  - It models one selected live registry client reference, bucket, flow name, version, intended root-child target name, and one direct Parameter Context reference.
  - It imports only that declared root-child process group, attaches or updates only the selected registry-backed version without provider write-back, records explicit ownership in the imported process-group comments, and does not add ongoing synchronization or generic graph editing.

- [platform-managed-versioned-flow-import-kind-values.yaml](platform-managed-versioned-flow-import-kind-values.yaml)
  - Kind overlay for platform-chart runtime-managed versioned-flow import.
  - Uses a single-node managed topology for the kind setup.
  - The command upgrades the platform release, waits for the live in-pod reconciler on pod `-0`, and then verifies that a later declared version change is applied without replacing the pod.
  - It verifies import, selected-version attachment, explicit ownership marking, and one seeded flow-content element on the imported process group.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml), [platform-fast-values.yaml](platform-fast-values.yaml), and [platform-managed-versioned-flow-import-values.yaml](platform-managed-versioned-flow-import-values.yaml).

- [platform-managed-versioned-flow-import-nifi-registry-values.yaml](platform-managed-versioned-flow-import-nifi-registry-values.yaml)
  - Runtime-managed NiFi Registry import for the standard `charts/nifi-platform` path.
  - It declares one `provider=nifiRegistry` client, one import source, one selected version, one intended root-child target name, and one direct Parameter Context reference.
  - In this path, the import bundle can create and reconcile the exact live NiFi Registry Flow Registry Client it owns.

- [platform-managed-versioned-flow-import-nifi-registry-kind-values.yaml](platform-managed-versioned-flow-import-nifi-registry-kind-values.yaml)
  - Kind overlay for platform-chart runtime-managed NiFi Registry compatibility.
  - Uses a real in-cluster `apache/nifi-registry` deployment, seeds an explicit historical version plus a later latest version, proves product-owned client recreation, proves explicit version import, and then proves live reconcile back to `latest` without replacing pod `-0`.
  - Compose it with [platform-managed-values.yaml](platform-managed-values.yaml), [platform-fast-values.yaml](platform-fast-values.yaml), and [platform-managed-versioned-flow-import-nifi-registry-values.yaml](platform-managed-versioned-flow-import-nifi-registry-values.yaml).

- [github-versioned-flow-selection-kind-values.yaml](github-versioned-flow-selection-kind-values.yaml)
  - Kind overlay for GitHub versioned-flow selection.
  - Compose it with [managed/values.yaml](managed/values.yaml), [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml), [github-flow-registry-kind-values.yaml](github-flow-registry-kind-values.yaml), [github-flow-registry-workflow-values.yaml](github-flow-registry-workflow-values.yaml), and [test-fast-values.yaml](test-fast-values.yaml).

There is also one shared NiFi `2.x` compatibility sweep for `charts/nifi-platform`.

For local kind runs:

- Compose with [platform-managed-values.yaml](platform-managed-values.yaml) and [platform-fast-values.yaml](platform-fast-values.yaml).
- The harness keeps the runtime checks shared and only changes the NiFi image tag inline per case.
- The default runtime sweep covers `apache/nifi:2.0.0` through `apache/nifi:2.8.0`.
- The sweep uses one shared kind cluster and verifies the managed install plus the secured health gate for each version.

The older app-chart NiFi `2.8.0` overlay still exists:

- [nifi-2.8.0-values.yaml](nifi-2.8.0-values.yaml)
  - Overrides the `charts/nifi` image tag to `apache/nifi:2.8.0`.
  - Uses `replicaCount: 2` for the older multi-node app-chart path.
  - Compose with either [standalone/values.yaml](standalone/values.yaml) or [managed/values.yaml](managed/values.yaml).
  - It also composes with the existing OIDC overlays for the `apache/nifi:2.8.0` OIDC path.

Only one authentication mode is supported at a time. The intended thin-platform combinations are:

- `singleUser + fileManaged`
- `oidc + externalClaimGroups`
- `ldap + ldapSync`

Preferred bootstrap:

- `authz.bootstrap.initialAdminGroup`
  - for OIDC
  - for LDAP on newer NiFi images when using the group-bootstrap path

Fallback bootstrap:

- `authz.bootstrap.initialAdminIdentity`
  - default for the baseline LDAP path on `apache/nifi:2.0.0`

Flow Registry Client notes:

- classic NiFi Registry is supported here for NiFi `2.x` environments, while Git-based Flow Registry Clients remain the preferred long-term direction
- Git-based Flow Registry Clients are preferred
- the `provider=nifiRegistry` path owns only the live Registry Client objects and imported flow instances it explicitly creates in the NiFi Registry compatibility workflow
- the chart renders a catalog under `flowRegistryClients.mountPath`
- the catalog is available as both `clients.yaml` and `clients.json`
- there is no controller-managed flow import or synchronization
- kind coverage includes the GitLab client path on NiFi `2.8.0` against a GitLab-compatible evaluator service
- kind coverage also includes the GitHub client path on NiFi `2.8.0` against a GitHub-compatible evaluator service with the fast profile
- kind coverage also includes a user-driven GitHub save-to-registry workflow on NiFi `2.8.0`
- kind coverage also includes the Bitbucket client path on NiFi `2.8.0` against a Bitbucket-compatible evaluator service with the fast profile

## Standalone

- [platform-standalone-values.yaml](platform-standalone-values.yaml)
  - Minimal one-release product-chart values for standalone mode.
  - Use with `charts/nifi-platform`.
  - The reusable app chart still comes from `charts/nifi`.

- [standalone/values.yaml](standalone/values.yaml)
  - Minimal app-chart values for a standalone NiFi 2 install on kind.
  - Use with `make helm-install-standalone`.

## Managed

- [secret-contracts/](secret-contracts/)
  - Copyable example Secret manifests for the explicit auth and TLS paths.
  - Shows the expected key layout for `nifi-auth`, `nifi-tls`, and `nifi-tls-params`.

- [platform-managed-cert-manager-quickstart-values.yaml](platform-managed-cert-manager-quickstart-values.yaml)
  - Standard one-release product-chart values for the cert-manager-first managed install path.
  - Generates `nifi-auth` and `nifi-tls-params` in the release namespace.
  - Those generated Secrets are preserved if you later upgrade in place to the explicit cert-manager path with the same Secret names.
  - Leaves cert-manager and the referenced issuer as prerequisites.

- [platform-managed-values.yaml](platform-managed-values.yaml)
  - Advanced one-release product-chart values for managed mode.
  - Installs the CRD, controller, RBAC, app chart, and `NiFiCluster` in one Helm release.
  - Uses explicit operator-provided `nifi-auth` and `nifi-tls` Secrets in the release namespace.
  - Requires the controller image to be reachable by the target cluster.

- [platform-managed-cert-manager-values.yaml](platform-managed-cert-manager-values.yaml)
  - Advanced one-release product-chart values for managed mode when cert-manager already exists in the cluster.
  - cert-manager remains a prerequisite and is not bundled by this chart.
  - Uses explicit operator-provided `nifi-auth` and `nifi-tls-params` Secrets in the release namespace.
  - This is the supported handoff target from the standard cert-manager quickstart path when you want explicit values-based ownership without changing Secret names.

- [platform-managed-quickstart-values.yaml](platform-managed-quickstart-values.yaml)
  - Secondary quickstart values for managed mode.
  - Generates the single-user bootstrap `nifi-auth` Secret and a self-signed `nifi-tls` Secret in the release namespace.
  - Reuses the generated quickstart secrets on upgrade.

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
