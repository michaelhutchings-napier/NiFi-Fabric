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

Current unit coverage in the scaffold includes:

- stable watched-resource hash calculation across input ordering
- revision drift detection
- config-triggered rollout planning from `status.rollout.startedAt`
- rollout blocked while the health gate is failing
- one-pod-at-a-time advancement
- safe resume from current status and StatefulSet state after controller restart
- ConfigMap drift triggering the managed `OnDelete` rollout path
- watched non-TLS Secret drift triggering the managed `OnDelete` rollout path
- TLS content drift entering and resolving through the autoreload observation window
- TLS content drift escalating to rollout when health degrades or policy requires it
- material TLS configuration drift triggering rollout immediately
- safe resume of TLS observation and TLS rollout after controller restart
- NiFi access-token and cluster-summary request handling

## controller-runtime `envtest`

`envtest` should cover:

- target resolution from `spec.targetRef.name`
- rejection of missing or invalid targets
- status updates for `TargetResolved`, `Available`, `Progressing`, `Degraded`, and `Hibernated`
- drift detection from watched Secrets and ConfigMaps
- persistence of `status.observedConfigHash` and `status.observedCertificateHash`
- persistence of `status.observedTLSConfigurationHash`
- persistence of `status.tls.observationStartedAt`, `status.tls.targetCertificateHash`, and `status.tls.targetTLSConfigurationHash`
- persistence of `status.rollout.startedAt` and `status.rollout.targetConfigHash`
- persistence of `status.rollout.targetCertificateHash` and `status.rollout.targetTLSConfigurationHash`
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
- TLS content drift resolving without restart when policy allows
- TLS configuration drift triggering a health-gated sequential rollout
- image or template upgrade through the `OnDelete` coordinator
- hibernation to zero and restore to the prior running size
- controller restart during rollout and during hibernation

## Upgrade, Restart, And Cert Rotation Cases

The minimum acceptance suite should include:

- no rollout begins while cluster health is failing
- no second pod deletion occurs before the prior pod is Ready and reconnected
- watched non-TLS drift uses the same restart path as StatefulSet revision drift
- TLS content drift advances `status.observedCertificateHash` only after the controller considers TLS state reconciled
- material TLS drift advances `status.observedCertificateHash` and `status.observedTLSConfigurationHash` only after rollout success
- hibernation preserves PVCs and restores `status.hibernation.lastRunningReplicas`
- rollout state resumes correctly after controller failure

## Test Environment Notes

- use `envtest` for reconciliation logic and status assertions
- use Helm template tests for values-to-manifest behavior
- use kind for end-to-end lifecycle behavior
- add AKS smoke validation after kind coverage is stable
