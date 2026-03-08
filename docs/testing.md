# Testing Strategy

## Goals

Testing must prove the thin-controller design is safe, idempotent, and understandable. The focus is on rollout safety, hibernation restore, and clear ownership boundaries between Helm, the controller, and NiFi.

## Unit Tests

Unit tests should cover:

- watched Secret and ConfigMap hash calculation
- revision and rollout predicates
- condition transition helpers
- `lastOperation` updates
- hibernation restore state handling
- TLS restart policy selection
- NiFi API client request and error handling

## controller-runtime `envtest`

`envtest` should cover:

- target resolution from `spec.targetRef.name`
- rejection of missing or invalid targets
- status updates for `TargetResolved`, `Available`, `Progressing`, `Degraded`, and `Hibernated`
- drift detection from watched Secrets and ConfigMaps
- blocked rollout when health gates fail
- backoff and retry behavior for NiFi API failures
- capture and restore of `status.hibernation.lastRunningReplicas`
- safe resume after controller restart during an in-flight operation

## Helm Template Tests

Helm template tests should cover:

- standalone chart rendering with no CRD dependency
- managed-mode rendering with `StatefulSet.updateStrategy=OnDelete`
- Services, PVCs, PDB, and `ServiceMonitor`
- RBAC needed for NiFi Kubernetes coordination and shared state
- cert-manager integration assumptions and Secret references
- scheduling fields such as affinity, tolerations, and topology spread
- OpenShift-friendly notes or templates without breaking AKS-first defaults

## kind Integration Tests

kind-based integration should cover:

- fresh multi-node NiFi cluster formation without ZooKeeper
- ConfigMap drift triggering a health-gated sequential rollout
- TLS Secret renewal with autoreload-first behavior
- TLS ref or path changes forcing a controlled restart
- image or template upgrade through the `OnDelete` coordinator
- hibernation to zero and restore to the prior running size
- controller restart during rollout and during hibernation

## Upgrade, Restart, And Cert Rotation Cases

The minimum acceptance suite should include:

- no rollout begins while cluster health is failing
- no second pod deletion occurs before the prior pod is Ready and reconnected
- TLS content-only drift can settle without restart when policy allows
- TLS drift forces restart when policy or health requires it
- hibernation preserves PVCs and restores `status.hibernation.lastRunningReplicas`
- rollout state resumes correctly after controller failure

## Test Environment Notes

- use `envtest` for reconciliation logic and status assertions
- use Helm template tests for values-to-manifest behavior
- use kind for end-to-end lifecycle behavior
- add AKS smoke validation after kind coverage is stable
