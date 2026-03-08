# TODO

## Current State

Completed in the scaffold:

1. Design pack and ADRs.
2. Go controller-runtime project skeleton.
3. `NiFiCluster` API types, CRD YAML, samples, and status helpers.
4. Status-only controller that resolves the target `StatefulSet`.
5. Standalone-first Helm chart with real NiFi 2 pod wiring, repository PVCs, TLS secrets, and kind workflow.
6. Example manifests, Makefile targets, kind config, and CI skeleton.

## Next Steps

1. Watch target `StatefulSet` and Pod changes instead of only reconciling on `NiFiCluster` events.
2. Replace the hand-written CRD and deepcopy scaffolding with generated artifacts once controller-tools are introduced.
3. Carry the documented standalone health gate into the controller before adding `OnDelete` rollout behavior.
4. Replace the local-development TLS Secret workflow with an optional cert-manager-backed chart path once the secret contract is stable.
5. Implement explicit condition transitions for rollout, cert drift, and hibernation progress.
6. Add the first real reconciliation loop for health-gated `OnDelete` rollout sequencing.
7. Add watched Secret and ConfigMap hash aggregation and status persistence.
8. Introduce a real NiFi API client and the offload or disconnect orchestration seam.
9. Implement policy-driven TLS drift handling and hibernation restore tracking.
10. Add controller deployment manifests and an image build workflow once the reconciler does real work.
11. Expand CI to include envtest assets and kind-based smoke coverage.

## Controller Health Gate Assumptions

Future managed restart, upgrade, cert, and hibernation logic should use the same gate proven by `hack/check-nifi-health.sh`:

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

- The controller needs a real watch model before more lifecycle logic can be trusted.
- Generated API artifacts should replace hand-maintained scaffolding before the schema grows.
- Rollout orchestration is the core reusable primitive for both upgrades and hibernation.
- TLS and hibernation logic should land only after restart sequencing is stable and test-covered.
