# ADR 0005: Upgrade and Rollout Strategy

- Status: Accepted
- Date: 2026-03-08

## Context

NiFi upgrades and configuration rollouts need health-gated sequencing. The default `RollingUpdate` behavior of a `StatefulSet` does not provide enough control over NiFi-specific offload, disconnect, and rejoin safety.

## Decision

Managed mode uses:

- `StatefulSet.updateStrategy=OnDelete`

Helm or GitOps updates the desired workload template and revision. The controller then:

- waits for cluster health gates
- sequences node offload or disconnect when required
- deletes one pod at a time, highest ordinal first by default
- waits for the replacement pod to become Ready
- waits for the NiFi node to reconnect before continuing

Offload and disconnect sequencing is controller-owned orchestration, not NiFi-native platform ownership.

## Consequences

- Template changes remain declarative in Git.
- Pod restart order and safety remain explicit and testable.
- The controller can resume safely after restarts by observing actual workload and status state.
