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
9. Example manifests, Makefile targets, kind config, and CI skeleton.

## Next Steps

1. Add offload or disconnect sequencing before pod deletion.
2. Implement hibernation and restore tracking without changing the rollout safety model.
3. Replace the hand-written CRD and deepcopy scaffolding with generated artifacts once controller-tools are introduced.
4. Replace the local-development TLS Secret workflow with an optional cert-manager-backed chart path once the secret contract is stable.
5. Expand CI to include envtest assets and kind-based smoke coverage.

## Current Managed Rollout Behavior

Current controller-owned mutations in managed mode:

- update `NiFiCluster.status`
- delete pods to advance a managed `OnDelete` rollout

Current rollout algorithm:

1. detect revision or template drift from the target `StatefulSet`
2. detect watched non-TLS drift from `spec.restartTriggers.configMaps[]` and non-TLS `spec.restartTriggers.secrets[]`
3. detect watched TLS drift and classify it as content-only or material TLS configuration drift
4. persist `status.observedConfigHash`, `status.observedCertificateHash`, and `status.observedTLSConfigurationHash`
5. for stable TLS content drift, observe autoreload first and only trigger rollout if policy or health requires it
6. wait for all target pods to become `Ready`
7. wait for the documented per-pod NiFi health gate to pass for multiple consecutive polls
8. choose the highest remaining ordinal in the current revision set
9. delete that pod
10. wait for replacement readiness and full cluster convergence
11. continue until the target revision or watched target hash is fully rolled out

What is still intentionally deferred:

- NiFi offload or disconnect sequencing
- hibernation
- controller metrics and events beyond the minimal runtime defaults

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

## Why This Order

- The rollout coordinator is now the core reusable primitive for upgrades, config drift, cert drift, and hibernation.
- Config and cert drift should layer onto the existing restart path instead of introducing parallel orchestration logic.
- Offload or disconnect sequencing should land before hibernation because both use the same per-pod lifecycle hook.
- Generated API artifacts should replace hand-maintained scaffolding before the status schema grows further.
