# Risks

## Top Architecture Risks

### Boundary Creep

Risk:

- the controller slowly absorbs more deployment configuration and starts to duplicate Helm

Mitigation:

- keep one CRD in MVP
- require an ADR and `docs/api.md` update for any new CRD or major field expansion
- reject feature additions that NiFi 2 already handles natively

### GitOps Ownership Confusion

Risk:

- GitOps tooling and the controller fight over the same fields

Mitigation:

- document exact controller-owned mutations
- keep controller ownership narrow
- require ignore-difference on `StatefulSet.spec.replicas` only for managed hibernation

## Complexity Risks

### Rollout Logic Becomes Fragile

Risk:

- restart sequencing grows complicated and hard to debug

Mitigation:

- use `OnDelete` and simple one-pod-at-a-time orchestration
- keep explicit status conditions and last-operation state
- test restart, timeout, and resume behavior with `envtest` and kind

### Cert Rotation Behavior Is Surprising

Risk:

- users do not know whether TLS drift will autoreload or restart

Mitigation:

- document a policy-driven strategy
- default to autoreload-first only when refs, paths, and passwords are stable
- surface the chosen action in status and events

## Operational Risks

### Controller To NiFi Authentication

Risk:

- the controller may fail to call secured NiFi APIs reliably

Mitigation:

- use a dedicated management identity
- keep stable TLS mount paths
- test expired, rotated, and mismatched credentials explicitly

### Hibernation Restore Mismatch

Risk:

- unhibernate restores the wrong replica count

Mitigation:

- persist `status.hibernation.lastRunningReplicas`
- record fallback behavior when the field is absent
- test controller restart during hibernation and restore

## Upgrade Risks

### NiFi 2 Minor Version Differences

Risk:

- Kubernetes coordination or TLS behavior may differ across NiFi 2.x releases

Mitigation:

- publish a supported version matrix
- pin tested versions in CI
- keep upgrade logic conservative and health-gated

## Compatibility Risks

### OpenShift Differences

Risk:

- OpenShift SCC, Routes, and defaults can diverge from AKS assumptions

Mitigation:

- treat OpenShift as friendly second target, not equal MVP scope
- document required adjustments
- add compatibility checks after AKS-first behavior is stable
