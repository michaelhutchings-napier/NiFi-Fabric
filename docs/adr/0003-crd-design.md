# ADR 0003: CRD Design

- Status: Accepted
- Date: 2026-03-08

## Context

The platform needs some operational API surface for status, orchestration intent, and rollout policy. The risk is introducing too many CRDs or allowing the CRD to mirror the entire Helm values structure.

## Decision

MVP will define one namespaced CRD:

- `NiFiCluster`

`NiFiCluster` is a thin operational API. It references an existing chart-managed workload and adds:

- desired runtime state such as `Running` or `Hibernated`
- watched Secret and ConfigMap references for restart orchestration
- small typed rollout and safety controls
- explicit status conditions and operation progress

The target reference shape is:

- `spec.targetRef.name`

For `v1alpha1`, the target kind is fixed to `StatefulSet` and is intentionally not user-configurable. This keeps the API small while leaving room for future expansion if a real need appears later.

## Rejected Alternatives

- multiple CRDs for flows, users, registry, policies, or backup
- a full cluster CRD that duplicates most Helm values
- a generic values blob inside the CRD

## Consequences

- Helm remains the main configuration surface.
- The controller API stays explainable.
- Future CRD additions require strong justification and should be treated as exceptions, not a default pattern.
