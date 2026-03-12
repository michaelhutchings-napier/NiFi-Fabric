# Testing Strategy

## Goals

Testing must prove the thin-controller design is safe, idempotent, and understandable. The focus is on rollout safety, hibernation restore, and clear ownership boundaries between Helm, the controller, and NiFi.

## Unit Tests

Unit tests should cover:

- watched Secret and ConfigMap hash calculation
- revision and rollout predicates
- condition transition helpers
- `lastOperation` updates
- hibernation restore state handling
- TLS restart policy selection
- NiFi API client request and error handling

Current unit coverage in the scaffold includes:

- stable watched-resource hash calculation across input ordering
- revision drift detection
- config-triggered rollout planning from `status.rollout.startedAt`
- rollout blocked while the health gate is failing
- one-pod-at-a-time advancement
- safe resume from current status and StatefulSet state after controller restart
- ConfigMap drift triggering the managed `OnDelete` rollout path
- watched non-TLS Secret drift triggering the managed `OnDelete` rollout path
- TLS content drift entering and resolving through the autoreload observation window
- TLS content drift escalating to rollout when health degrades or policy requires it
- material TLS configuration drift triggering rollout immediately
- safe resume of TLS observation and TLS rollout after controller restart
- capture of `status.hibernation.lastRunningReplicas` before managed scale-to-zero
- restore to the prior running replica count
- fallback restore to `1` replica when no prior running size was recorded
- safe resume while hibernation is already waiting for pods to terminate
- restore remaining `Progressing=True` until stable health returns
- rollout waits for NiFi disconnect and offload before deleting the target pod
- hibernation removes the highest ordinal node only after NiFi disconnect and offload complete
- node-preparation timeout keeps `status.nodeOperation` persisted and blocks destructive progress
- NiFi access-token and cluster-summary request handling
- lifecycle transition metrics for rollout, TLS observation, hibernation, and node-preparation retry or timeout paths

## controller-runtime `envtest`

`envtest` should cover:

- target resolution from `spec.targetRef.name`
- rejection of missing or invalid targets
- status updates for `TargetResolved`, `Available`, `Progressing`, `Degraded`, and `Hibernated`
- drift detection from watched Secrets and ConfigMaps
- persistence of `status.observedConfigHash` and `status.observedCertificateHash`
- persistence of `status.observedTLSConfigurationHash`
- persistence of `status.tls.observationStartedAt`, `status.tls.targetCertificateHash`, and `status.tls.targetTLSConfigurationHash`
- persistence of `status.rollout.startedAt` and `status.rollout.targetConfigHash`
- persistence of `status.rollout.targetCertificateHash` and `status.rollout.targetTLSConfigurationHash`
- blocked rollout when health gates fail
- backoff and retry behavior for NiFi API failures
- capture and restore of `status.hibernation.lastRunningReplicas`
- safe resume after controller restart during an in-flight operation

## Helm Template Tests

Helm template tests should cover:

- standalone chart rendering with no CRD dependency
- one-release platform-chart rendering for `standalone`, `managed`, and `managed-cert-manager`
- managed-mode rendering with `StatefulSet.updateStrategy=OnDelete`
- Services, PVCs, PDB, and `ServiceMonitor`
- RBAC needed for NiFi Kubernetes coordination and shared state
- cert-manager integration assumptions and Secret references
- scheduling fields such as affinity, tolerations, and topology spread
- OpenShift-friendly notes or templates without breaking AKS-first defaults

## kind Integration Tests

kind-based integration should cover:

- a single fresh-kind `make kind-alpha-e2e` path for private-alpha validation
- a focused fresh-kind `make kind-platform-managed-fast-e2e` path that installs the one-release platform chart in managed mode and proves CRD, controller, app, and `NiFiCluster` startup on the fast profile
- a focused fresh-kind `make kind-platform-managed-cert-manager-fast-e2e` path that verifies the platform chart fails clearly before cert-manager exists, then succeeds once cert-manager is bootstrapped, all on the fast profile
- additive focused fast-profile reruns that keep NiFi multi-node but reduce the shape to the smallest stable two-node footprint for local kind validation
- a separate `make kind-bootstrap-cert-manager` path that installs cert-manager from the official Helm chart source and bootstraps the evaluator issuer flow without modifying the NiFi chart
- a focused fresh-kind `make kind-cert-manager-e2e` path for cert-manager validation without changing the main alpha gate
- a focused fresh-kind `make kind-cert-manager-nifi-2-8-e2e` path for cert-manager validation on NiFi `2.8.0`
- a focused fresh-kind `make kind-cert-manager-nifi-2-8-fast-e2e` path for cert-manager validation on NiFi `2.8.0` with the additive fast profile
- a focused `make kind-auth-oidc-e2e` path for OIDC runtime validation without pulling in the main lifecycle gate
- a focused `make kind-auth-oidc-nifi-2-8-fast-e2e` path for OIDC runtime validation on NiFi `2.8.0` with the additive fast profile
- a focused `make kind-auth-ldap-e2e` path for LDAP runtime validation without pulling in the main lifecycle gate
- a focused `make kind-nifi-2-8-e2e` path for a newer NiFi 2.x managed compatibility proof without rerunning the full alpha gate
- a focused `make kind-flow-registry-gitlab-e2e` path for GitLab Flow Registry Client runtime on NiFi `2.8.0` without pulling in the full alpha gate
- a focused `make kind-flow-registry-github-fast-e2e` path for GitHub Flow Registry Client runtime on NiFi `2.8.0` with the additive fast profile
- preloading the NiFi runtime image into the fresh kind node so alpha validation is not gated by an in-cluster registry pull
- phase-level fresh-kind reruns:
  - `make kind-e2e-rollout`
  - `make kind-e2e-config-drift`
  - `make kind-e2e-tls`
  - `make kind-e2e-hibernate`
- fresh multi-node NiFi cluster formation without ZooKeeper
- ConfigMap drift triggering a health-gated sequential rollout
- TLS content drift resolving without restart when policy allows
- TLS configuration drift triggering a health-gated sequential rollout
- cert-manager installation, issuer bootstrap, and `Certificate` readiness on kind
- cert-manager renewal updating the mounted Secret without forcing restart when refs, paths, and passwords remain stable
- restart-required TLS config change continuing to use the managed rollout path even when the TLS Secret is cert-manager-managed
- Keycloak bootstrap, NiFi OIDC discovery and login wiring, exact group-claim prerequisites, Initial Admin Identity fallback bootstrap, and non-admin denial checks
- Keycloak bootstrap, NiFi OIDC discovery and login wiring, exact identifying-user and groups claim wiring, seeded NiFi application-group prerequisites, Initial Admin Identity fallback bootstrap, and non-admin denial checks on NiFi `2.8.0`
- LDAP bootstrap, NiFi LDAP login and LDAP user or group provider wiring, Initial Admin Identity bootstrap, and non-admin denial checks
- GitLab-compatible evaluator bootstrap, NiFi GitLab Flow Registry Client creation through NiFi's own API, and bucket listing on NiFi `2.8.0`
- GitHub-compatible evaluator bootstrap, NiFi GitHub Flow Registry Client creation through NiFi's own API, and bucket listing on NiFi `2.8.0` with the fast profile
- image or template upgrade through the `OnDelete` coordinator
- hibernation to zero and restore to the prior running size
- controller restart during rollout and during hibernation
- controller metrics exposure for rollout, TLS, hibernation, and node-preparation counters
- Kubernetes events for `NiFiCluster` lifecycle transitions

## Upgrade, Restart, And Cert Rotation Cases

The minimum acceptance suite should include:

- no rollout begins while cluster health is failing
- no second pod deletion occurs before the prior pod is Ready and reconnected
- watched non-TLS drift uses the same restart path as StatefulSet revision drift
- TLS content drift advances `status.observedCertificateHash` only after the controller considers TLS state reconciled
- material TLS drift advances `status.observedCertificateHash` and `status.observedTLSConfigurationHash` only after rollout success
- hibernation preserves PVCs and restores `status.hibernation.lastRunningReplicas`
- rollout state resumes correctly after controller failure

## Test Environment Notes

- use `envtest` for reconciliation logic and status assertions
- use Helm template tests for values-to-manifest behavior
- use kind for end-to-end lifecycle behavior
- add AKS smoke validation after kind coverage is stable
- keep [docs/aks.md](aks.md) and `examples/aks/*` honest: prepared for evaluation, not validated, until a real AKS run is recorded
- keep [docs/openshift.md](openshift.md) and `examples/openshift/*` honest: prepared for evaluation, not validated, until a real OpenShift run is recorded

Current alpha note:

- the repo now has a green fresh-kind private-alpha workflow
- the repo now has a render-validated `charts/nifi-platform` install path for one-release standalone, managed, and managed-cert-manager installs
- the repo now also has green focused `make kind-platform-managed-fast-e2e` and `make kind-platform-managed-cert-manager-fast-e2e` workflows for the product-chart install path
- the repo now also has green fresh-kind `make kind-cert-manager-e2e` and `make kind-cert-manager-nifi-2-8-e2e` workflows
- the repo now also has a green focused `make kind-cert-manager-nifi-2-8-fast-e2e` workflow for cert-manager on NiFi `2.8.0` with the fast profile
- the repo now also has green focused `make kind-auth-oidc-e2e` and `make kind-auth-ldap-e2e` workflows
- the repo now also has a green focused `make kind-auth-oidc-nifi-2-8-fast-e2e` workflow for OIDC on NiFi `2.8.0` with the fast profile
- the repo now also has a green focused `make kind-nifi-2-8-e2e` workflow for the newer NiFi 2.x proof target
- the repo now also has a focused `make kind-flow-registry-gitlab-e2e` workflow for GitLab Flow Registry Client runtime on NiFi `2.8.0`
- the repo now also has a green focused `make kind-flow-registry-github-fast-e2e` workflow for GitHub Flow Registry Client runtime on NiFi `2.8.0` with the fast profile
- CI should treat `make kind-alpha-e2e` as the gate and use the phase-level targets for faster diagnosis
- evaluator-facing examples and quickstarts should stay aligned with that same gate
- cert-manager mode should still render in CI via `helm template`
- the focused cert-manager path is an additional evaluation workflow, not a replacement for `make kind-alpha-e2e`
- the focused auth paths are additional evaluator workflows, not replacements for `make kind-alpha-e2e`
- the focused newer-version path is an additional compatibility workflow, not a replacement for `make kind-alpha-e2e`
- the focused GitLab Flow Registry path is an additional runtime workflow, not a replacement for `make kind-alpha-e2e`
- focused fast-profile commands are additional local-cost controls, not replacements for the baseline profiles or the alpha gate
- cert-manager itself remains a cluster dependency and should stay outside the NiFi chart
- CI diagnostics should include compact `NiFiCluster` status, compact `StatefulSet` status, pod revision or UID state, recent events, controller logs, and a controller metrics snapshot before falling back to large YAML dumps
