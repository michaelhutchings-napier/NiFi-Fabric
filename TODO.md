# TODO

## Current State

Completed in the scaffold:

1. Design pack and ADRs.
2. Go controller-runtime project skeleton.
3. `NiFiCluster` API types, CRD YAML, samples, and status helpers.
4. Managed-mode controller that watches the target `StatefulSet` and Pods, evaluates the documented per-pod NiFi health gate, and coordinates `OnDelete` rollout sequencing.
5. Standalone-first Helm chart with real NiFi 2 pod wiring, repository PVCs, TLS secrets, and kind workflow.
6. Minimal controller image and deployment path for local kind verification.
7. Watched ConfigMap and Secret hash aggregation with persisted `observedConfigHash`, `observedCertificateHash`, and managed config-drift rollout triggers.
8. Policy-driven TLS drift handling with autoreload observation, restart-required TLS rollout decisions, and persisted TLS status.
9. Managed hibernation and restore with persisted `status.hibernation.lastRunningReplicas` and health-gated restore completion.
10. Example manifests, Makefile targets, kind config, and CI skeleton.
11. Optional chart-managed cert-manager TLS source with a focused fresh-kind `make kind-cert-manager-e2e` proof path.
12. Separate kind bootstrap path for cert-manager and evaluator issuer setup using the official Helm chart source.

## Next Steps

1. Keep `make kind-alpha-e2e` green as the private-alpha gate before broadening scope.
2. Replace the hand-written CRD and deepcopy scaffolding with generated artifacts once controller-tools are introduced.
3. Decide whether the focused `make kind-cert-manager-e2e` path should stay local-only or later be promoted into a separate scheduled CI job without changing the main alpha gate.
4. Keep `make kind-load-nifi-image` aligned with the chart NiFi image tag so fresh-kind alpha runs stay repeatable.
5. Decide on the final repo and module naming before the first non-alpha tag.
6. Keep README quickstarts, [examples/README.md](examples/README.md), `docs/local-kind.md`, and the cert-manager overlay docs aligned with the exact alpha gate and manual cert-manager commands.
7. Decide whether trust-manager is needed later as an optional extension for broader CA bundle distribution without changing the current chart or controller scope.
8. Run the first real AKS and OpenShift evaluations before claiming anything beyond kind-based private-alpha coverage.

## Current Managed Rollout Behavior

Current controller-owned mutations in managed mode:

- update `NiFiCluster.status`
- delete pods to advance a managed `OnDelete` rollout
- update `StatefulSet.spec.replicas` for hibernation and restore only
- call the NiFi API to disconnect and offload the node that is about to be deleted or scaled away

Current rollout algorithm:

1. detect revision or template drift from the target `StatefulSet`
2. detect watched non-TLS drift from `spec.restartTriggers.configMaps[]` and non-TLS `spec.restartTriggers.secrets[]`
3. detect watched TLS drift and classify it as content-only or material TLS configuration drift
4. persist `status.observedConfigHash`, `status.observedCertificateHash`, and `status.observedTLSConfigurationHash`
5. for stable TLS content drift, observe autoreload first and only trigger rollout if policy or health requires it
6. wait for all target pods to become `Ready`
7. wait for the documented per-pod NiFi health gate to pass for multiple consecutive polls
8. choose the highest remaining ordinal in the current revision set
9. persist `status.nodeOperation` and ask NiFi to disconnect and offload that node
10. delete that pod only after NiFi reports the node as safe to remove
11. wait for replacement readiness and full cluster convergence
12. continue until the target revision or watched target hash is fully rolled out

What is still intentionally deferred:

- richer restore target memory than `status.hibernation.lastRunningReplicas` with a `1` replica fallback

Current alpha gate:

- `make kind-alpha-e2e` is the private-alpha release gate and currently passes on a fresh kind cluster
- `make kind-e2e-rollout`, `make kind-e2e-config-drift`, `make kind-e2e-tls`, and `make kind-e2e-hibernate` are the intended first-line debugging targets
- lifecycle events and metrics are now part of the supported debug surface and should stay stable while the alpha gate stays green
- future alpha work should preserve those gates before adding scope

Current watched-drift assumptions:

- all watched ConfigMaps contribute to config drift
- the watched Secret that matches the target StatefulSet TLS mount contributes to certificate drift
- all other watched Secrets contribute to config drift
- config drift persists `status.rollout.startedAt` and `status.rollout.targetConfigHash` so controller restarts resume cleanly
- stable TLS content drift persists `status.tls.observationStartedAt`, `status.tls.targetCertificateHash`, and `status.tls.targetTLSConfigurationHash` so observation resumes cleanly
- material TLS drift and restart-required TLS policy decisions persist `status.rollout.targetCertificateHash` and `status.rollout.targetTLSConfigurationHash` so rollout resumes cleanly

## Controller Health Gate Assumptions

Managed restart, upgrade, cert, and hibernation logic should use the same gate proven by `hack/check-nifi-health.sh`:

1. The target `StatefulSet` has the expected number of `Ready` pods.
2. Each pod can mint a local token against its own HTTPS endpoint.
3. Each pod's own `flow/cluster/summary` reports:
   - `clustered=true`
   - `connectedToCluster=true`
   - `connectedNodeCount == expected replicas`
   - `totalNodeCount == expected replicas`
4. The convergence result must hold for multiple consecutive polls before the controller advances a destructive step.

Operational assumption:

- `Ready=True` alone is not a safe rollout gate.
- `Ready=True` plus per-pod API reachability is a useful fallback diagnostic signal, but not a safe rollout gate.
- The controller should requeue through the post-startup observation window instead of treating early `0 / N` or `connected=false` summaries as immediate failures.

## Current Managed Hibernation Behavior

Current hibernation algorithm:

1. `spec.desiredState=Hibernated` captures `status.hibernation.lastRunningReplicas` if it is still empty.
2. If `spec.safety.requireClusterHealthy=true`, the controller waits for the documented per-pod health gate before scaling down.
3. The controller chooses the highest ordinal remaining pod.
4. The controller persists `status.nodeOperation` and asks NiFi to disconnect that node.
5. Once NiFi reports `DISCONNECTED`, the controller asks NiFi to offload that node.
6. Once NiFi reports `OFFLOADED`, the controller reduces `StatefulSet.spec.replicas` by one.
7. After each scale-down, the controller treats the new `StatefulSet.spec.replicas` value as the next intermediate target, waits for live pods and health to settle against that new count, clears completed `status.nodeOperation`, and then selects the next highest ordinal from live state.
8. The controller repeats the sequence until the cluster reaches zero replicas, then sets `ConditionHibernated=True`.
9. `spec.desiredState=Running` restores `StatefulSet.spec.replicas` from `status.hibernation.lastRunningReplicas`.
10. If that field is absent, the controller restores to `1` replica.
11. Restore success is blocked until pod readiness and the stable per-pod NiFi convergence gate return.

## Why This Order

- The rollout coordinator is now the core reusable primitive for upgrades, config drift, cert drift, and hibernation restore gating.
- Config and cert drift should layer onto the existing restart path instead of introducing parallel orchestration logic.
- Generated API artifacts should replace hand-maintained scaffolding before the status schema grows further.
