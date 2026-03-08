# ADR 0002: Helm vs Controller Boundaries

- Status: Accepted
- Date: 2026-03-08

## Context

The platform needs both strong GitOps ergonomics and safe runtime orchestration. If Helm owns too much, safe restart and hibernation behavior become fragile. If the controller owns too much, the project becomes an operator-heavy system with duplicate configuration APIs.

## Decision

Helm is the authoritative owner for standard Kubernetes resources and NiFi configuration templating.

The controller is limited to lifecycle and safety behavior that requires observing live state and performing ordered actions.

The chart must remain installable without the controller.

In managed mode, the controller owns only these mutations:

- writes to `NiFiCluster.status`
- pod deletions to advance `StatefulSet.updateStrategy=OnDelete`
- updates to `StatefulSet.spec.replicas` for hibernation and restore only
- NiFi API calls for node offload and disconnect sequencing

The controller does not:

- install or template workloads
- manage Helm releases
- mutate arbitrary `StatefulSet` template fields
- become a second deployment-values interface

## GitOps Implications

Managed hibernation requires a narrow ownership exception:

- GitOps tooling should ignore drift on `StatefulSet.spec.replicas` for managed clusters that use hibernation

GitOps tooling should not ignore:

- pod template drift
- image drift
- ConfigMap or Secret reference drift
- controller status updates on the `NiFiCluster`

## Consequences

- Users can start with Helm-only installs and adopt the controller later.
- Rollout safety is explicit rather than hidden inside Helm hooks or external scripts.
- GitOps ownership remains mostly unchanged, with one documented exception for hibernation.
