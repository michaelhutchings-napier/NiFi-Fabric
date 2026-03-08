# TODO

## Current State

Completed in the scaffold:

1. Design pack and ADRs.
2. Go controller-runtime project skeleton.
3. `NiFiCluster` API types, CRD YAML, samples, and status helpers.
4. Status-only controller that resolves the target `StatefulSet`.
5. Minimal standalone Helm chart and managed-mode chart switch.
6. Example manifests, Makefile targets, kind config, and CI skeleton.

## Next Steps

1. Watch target `StatefulSet` and Pod changes instead of only reconciling on `NiFiCluster` events.
2. Replace the hand-written CRD and deepcopy scaffolding with generated artifacts once controller-tools are introduced.
3. Expand the Helm chart from a renderable scaffold into a working NiFi 2 deployment with finalized TLS and repository wiring.
4. Implement explicit condition transitions for rollout, cert drift, and hibernation progress.
5. Add the first real reconciliation loop for health-gated `OnDelete` rollout sequencing.
6. Add watched Secret and ConfigMap hash aggregation and status persistence.
7. Introduce a real NiFi API client and the offload or disconnect orchestration seam.
8. Implement policy-driven TLS drift handling and hibernation restore tracking.
9. Add controller deployment manifests and an image build workflow once the reconciler does real work.
10. Expand CI to include envtest assets and kind-based smoke coverage.

## Why This Order

- The controller needs a real watch model before more lifecycle logic can be trusted.
- Generated API artifacts should replace hand-maintained scaffolding before the schema grows.
- Rollout orchestration is the core reusable primitive for both upgrades and hibernation.
- TLS and hibernation logic should land only after restart sequencing is stable and test-covered.
