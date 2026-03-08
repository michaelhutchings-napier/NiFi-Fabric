# Reconciliation

## Overview

The controller stays small by splitting behavior into a few narrow, idempotent loops. Every loop works against the same namespace-scoped target and uses observed state rather than assumptions.

## Reconciliation Loops

| Loop | Inputs | Main actions |
| --- | --- | --- |
| Target resolution and validation | `NiFiCluster`, target `StatefulSet` | resolve `spec.targetRef.name`, verify same-namespace `StatefulSet`, set `TargetResolved` |
| Watched-resource hash aggregation | referenced Secrets and ConfigMaps, target pod template | compute aggregate config and certificate hashes, compare with status |
| Rollout coordinator | `StatefulSet` revision drift, hash drift, pod readiness, NiFi node state | gate rollout, sequence offload or disconnect, delete pods one at a time |
| Hibernation coordinator | desired state, current replicas, NiFi node state | capture prior replica count, offload or disconnect nodes, scale to zero, restore later |
| Status and condition sync | all observed objects and controller operations | update conditions, replica counts, node counts, last operation, hashes |

## Watched Resources

The controller watches:

- `NiFiCluster`
- the target `StatefulSet`
- Pods owned by the target `StatefulSet`
- Secrets referenced by `spec.restartTriggers.secrets`
- ConfigMaps referenced by `spec.restartTriggers.configMaps`

Services are read for discovery and addressing but do not drive separate reconciliation logic.

Watched input classification is intentionally simple:

- every referenced ConfigMap contributes to config drift
- a referenced Secret contributes to certificate drift only when its name matches the TLS Secret mounted by the target StatefulSet
- every other referenced Secret contributes to config drift

## Event Triggers

The main triggers are:

- `NiFiCluster.metadata.generation` changes
- target `StatefulSet` revision or replica changes
- Pod readiness or deletion events
- watched Secret or ConfigMap changes
- NiFi node state changes observed during active rollout or hibernation work

## Idempotency Strategy

Idempotency rules are strict:

- aggregate config and certificate hashes are written to status only after the controller has observed the related state transition
- one operation is active per cluster at a time
- pod deletion is never repeated until the expected pod and NiFi node state changes are observed
- config-triggered rollout progress is resumed from `status.rollout.startedAt` and `status.rollout.targetConfigHash`
- `status.hibernation.lastRunningReplicas` is captured before the first scale-down below the current running size
- unhibernate uses recorded status rather than guessing from history or annotations

## Failure Handling

Failure handling is explicit:

- target resolution failures set `TargetResolved=False`
- unhealthy cluster gates set `Progressing=False` and `Degraded=True`
- NiFi API failures preserve the current pod set and requeue with backoff
- timeouts during offload or disconnect use a documented failure policy and never silently continue
- controller restarts resume from current object state and status fields

## Backoff And Requeue Logic

- use fast requeue while an operation is actively progressing
- use exponential backoff with jitter for NiFi API, network, or authentication failures
- requeue immediately after a pod deletion until the replacement pod reaches the expected state
- do not start a new rollout while health gates fail

## Cluster Health Gate

The controller should use the same convergence gate that the standalone verification flow uses.

Kubernetes signals:

- target `StatefulSet.spec.replicas` matches the expected running size
- every target pod reports `Ready=True`

NiFi signals:

- each pod can mint a local token against its own HTTPS endpoint
- each pod's own `flow/cluster/summary` reports:
  - `clustered=true`
  - `connectedToCluster=true`
  - `connectedNodeCount == expected replicas`
  - `totalNodeCount == expected replicas`

Stability rule:

- the NiFi convergence result must hold for multiple consecutive polls before the controller advances restart, upgrade, or hibernation work

Important constraints:

- do not use the ClusterIP Service as the authoritative convergence view because it hides which node answered
- do not assume a token minted on one node is reusable against another node
- `Ready=True` alone is not sufficient for destructive orchestration

Observation window behavior:

- a fresh cluster can reach `Ready=True` before NiFi reports full membership
- each pod's secured API can become reachable before `Ready=True`
- during that gap, the controller should requeue and keep `Progressing=True`
- if pods are ready and the secured API is reachable but `flow/cluster/summary` is still lagging, treat that only as a fallback diagnostic signal

## Safe Restart Orchestration

Managed restart behavior is:

1. detect revision or restart-trigger drift
2. confirm the cluster is healthy if `spec.safety.requireClusterHealthy=true`
3. choose the next highest ordinal remaining in the current revision set
4. delete the pod
5. wait for the replacement pod to become Ready
6. wait for the replacement pod's secured NiFi API to become reachable
7. wait for full-cluster convergence and stable health polls
8. record progress and continue

The controller owns the sequencing. NiFi owns the cluster behavior that follows each deletion.

Current implementation notes:

- managed mode is limited to `StatefulSet.updateStrategy=OnDelete`
- the controller uses `StatefulSet.status.currentRevision`, `updateRevision`, and `currentReplicas` as the primary ordinal-planning signal
- config-triggered rollouts persist `status.rollout.trigger=ConfigDrift`, `status.rollout.startedAt`, and `status.rollout.targetConfigHash`
- during a config-triggered rollout, a pod counts as updated once it has been recreated after `status.rollout.startedAt`
- if `currentRevision` lags briefly after all pods are healthy on the target revision, the controller treats the rollout as complete once the pods and health gate are converged
- offload or disconnect sequencing is intentionally deferred to a later slice

## Cert Hash And Config Hash Logic

Config and certificate handling use separate aggregate hashes:

- non-TLS config drift triggers a controlled rolling restart
- TLS Secret content drift is detected and stored separately in status
- TLS restart policy decisions are intentionally deferred to the next slice
- `spec.restartPolicy.tlsDrift` is not applied yet

Current implementation details:

- `status.observedConfigHash` tracks the last config hash that has been fully applied to a healthy cluster
- `status.observedCertificateHash` tracks the last TLS hash that the controller has accepted as steady state
- if config drift is detected, the controller captures the desired target in `status.rollout.targetConfigHash` and advances the existing managed `OnDelete` rollout path
- if certificate drift is detected, the controller records that drift in conditions and leaves the observed certificate hash unchanged
- if watched inputs change again during a config rollout, the controller finishes the in-flight rollout against the recorded target hash and then detects the new drift on the next reconcile

## Hibernation And Restore

Hibernation behavior is:

1. `spec.desiredState=Hibernated`
2. record `status.hibernation.lastRunningReplicas`
3. offload or disconnect the highest ordinal node
4. reduce `StatefulSet.spec.replicas` by one
5. repeat until replicas reach zero
6. set `Hibernated=True`

Restore behavior is:

1. `spec.desiredState=Running`
2. restore `StatefulSet.spec.replicas` to `status.hibernation.lastRunningReplicas`
3. wait for pods to become Ready
4. wait for NiFi nodes to reconnect
5. clear hibernation progress once the prior running size is reached

If `status.hibernation.lastRunningReplicas` is absent, the controller falls back to the documented baseline desired replica count from the workload flow and records that fallback in status.

## Conditions And State Transitions

| Condition | True when | False when |
| --- | --- | --- |
| `TargetResolved` | target `StatefulSet` exists and is valid | target is missing or invalid |
| `Available` | desired state is satisfied and cluster is healthy | pods or nodes are not sufficiently healthy |
| `Progressing` | rollout, hibernation, restore, or initial convergence is active | no active transition is underway |
| `Degraded` | safety checks or operations are blocked or failing | no active degradation is present |
| `Hibernated` | desired and observed replicas are zero and hibernation completed | cluster is running or being restored |

`Progressing=True` remains set during restore from hibernation until the prior running replica count is reached and the cluster nodes reconnect.

## Notes For Tests

The controller logic should be tested for:

- repeated events causing no duplicate pod deletion
- controller restart during rollout
- controller restart during config-triggered rollout
- controller restart during hibernation
- timeout during offload
- TLS drift detected without restart before policy handling is implemented
- watched non-TLS Secret and ConfigMap changes triggering the same rollout path used for StatefulSet drift
