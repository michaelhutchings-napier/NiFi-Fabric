# TODO

## Prioritized Backlog

1. Write and review the design pack.
2. Scaffold the standalone Helm chart for NiFi 2.x.
3. Add managed-mode chart switches for `OnDelete`, controller integration, and documented GitOps ownership boundaries.
4. Define the `NiFiCluster` CRD, samples, and condition model.
5. Implement target resolution and status-only reconciliation.
6. Implement health-gated `OnDelete` rollout orchestration.
7. Implement watched Secret and ConfigMap hash detection.
8. Implement policy-driven cert rotation handling.
9. Implement hibernation and restore tracking with `status.hibernation.lastRunningReplicas`.
10. Add unit, Helm, `envtest`, and kind coverage.
11. Add AKS validation guidance.
12. Add OpenShift-friendly notes and validation guidance.

## Why This Order

- The chart must exist before the controller can safely target a real workload shape.
- The CRD and status model must exist before lifecycle orchestration can be tested.
- Restart orchestration comes before hibernation because it is the simpler reusable primitive.
- Hibernation depends on the same safety checks and NiFi API sequencing as restart logic.
- Platform validation should follow after the core behavior is stable and testable.
