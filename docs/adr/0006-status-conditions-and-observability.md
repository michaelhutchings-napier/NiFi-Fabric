# ADR 0006: Status Conditions and Observability

- Status: Accepted
- Date: 2026-03-08

## Context

Operators and GitOps users need clear signals about whether a cluster is healthy, progressing, degraded, or hibernated. A single broad phase field is usually too coarse for debugging and automation.

The platform also needs to remember the prior running replica count so hibernation can restore safely without guessing.

## Decision

`NiFiCluster` status is condition-first.

The minimum condition set is:

- `TargetResolved`
- `SecretsReady`
- `TLSMaterialReady`
- `Available`
- `Progressing`
- `Degraded`
- `Hibernated`

Status also records:

- observed generation and observed `StatefulSet` revision
- aggregate config and certificate hashes
- current TLS decision state in `status.tls`
- desired, ready, and updated replica counts
- connected, disconnected, and offloaded NiFi node counts
- last operation details
- `status.hibernation.lastRunningReplicas`

The controller should emit Kubernetes events and expose controller metrics for reconciliation errors, rollout progress, and hibernation transitions.

`SecretsReady` and `TLSMaterialReady` are observation-only conditions. They tell users whether referenced Secret inputs and workload TLS material are present and structurally usable, including the standard `nifi-auth`, `nifi-tls`, and `nifi-tls-params` contracts when those inputs are in use. They do not imply controller ownership of those Secrets.

`status.tls` is also observation-first. It reports the current TLS decision state for managed running clusters, including whether the controller is idle, actively observing autoreload, or has determined that a controlled restart rollout is required. This is intended to make TLS behavior legible in `kubectl get -o yaml` and `kubectl describe` without expanding controller ownership of TLS materials.

## Consequences

- Users get explicit machine-readable status without relying on logs.
- Unhibernate can restore the prior running size safely.
- A large phase enum is unnecessary and intentionally avoided.
