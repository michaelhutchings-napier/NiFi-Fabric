# API

## Proposed CRD

MVP defines one namespaced CRD:

- `NiFiCluster`

Working API version for the design pack:

- `platform.nifi.io/v1alpha1`

## Resource Shape

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: example
  namespace: nifi
spec:
  targetRef:
    name: nifi
  desiredState: Running
  suspend: false
  restartTriggers:
    configMaps: []
    secrets: []
  restartPolicy:
    tlsDrift: AutoreloadThenRestartOnFailure
  rollout:
    minReadySeconds: 30
    podReadyTimeout: 10m
    clusterHealthTimeout: 15m
  hibernation:
    offloadTimeout: 5m
  safety:
    requireClusterHealthy: true
status:
  observedGeneration: 1
  observedStatefulSetRevision: nifi-67c9c7c966
  observedConfigHash: sha256:...
  observedCertificateHash: sha256:...
  observedTLSConfigurationHash: sha256:...
  tls:
    observationStartedAt: "2026-03-08T10:15:00Z"
    targetCertificateHash: sha256:...
    targetTLSConfigurationHash: sha256:...
  rollout:
    trigger: TLSDrift
    startedAt: "2026-03-08T10:15:00Z"
    targetCertificateHash: sha256:...
    targetTLSConfigurationHash: sha256:...
  replicas:
    desired: 3
    ready: 3
    updated: 3
  clusterNodes:
    connected: 3
    disconnected: 0
    offloaded: 0
  hibernation:
    lastRunningReplicas: 3
  nodeOperation:
    purpose: Restart
    podName: nifi-2
    nodeId: 7f9d2f38-f07d-4c95-8d84-0ef5872667e4
    stage: Offloading
    startedAt: "2026-03-08T10:18:00Z"
  lastOperation:
    type: Rollout
    phase: Succeeded
    startedAt: "2026-03-08T10:15:00Z"
    completedAt: "2026-03-08T10:22:00Z"
    message: All pods updated to the desired revision
  autoscaling:
    recommendedReplicas: 2
    reason: LowPressureDetected
    lowPressureSince: "2026-03-08T10:40:00Z"
    lowPressure:
      since: "2026-03-08T10:40:00Z"
      lastObservedAt: "2026-03-08T10:40:30Z"
      consecutiveSamples: 3
      requiredConsecutiveSamples: 3
      flowFilesQueued: 0
      bytesQueued: 0
      bytesQueuedObserved: true
      message: zero backlog observed across 3/3 consecutive evaluations
    execution:
      phase: ScaleDownPrepare
      state: Blocked
      startedAt: "2026-03-08T10:42:00Z"
      lastTransitionTime: "2026-03-08T10:45:00Z"
      targetReplicas: 2
      message: timed out waiting for NiFi node node-2 to reach OFFLOADED before proceeding
      blockedReason: NodePreparationTimedOut
  conditions: []
```

## Spec Schema

| Field | Type | Required | Purpose |
| --- | --- | --- | --- |
| `spec.targetRef.name` | string | yes | target workload name in the same namespace |
| `spec.desiredState` | enum `Running|Hibernated` | yes | desired runtime state |
| `spec.suspend` | bool | yes | pause active reconciliation without deleting status |
| `spec.restartTriggers.configMaps[]` | list of object refs | yes | watched ConfigMaps that should participate in restart decisions |
| `spec.restartTriggers.secrets[]` | list of object refs | yes | watched Secrets that should participate in restart decisions |
| `spec.restartPolicy.tlsDrift` | enum | yes | policy for TLS content drift handling |
| `spec.rollout.minReadySeconds` | integer | yes | minimum ready period before advancing rollout |
| `spec.rollout.podReadyTimeout` | duration | yes | timeout for a replacement pod to become Ready |
| `spec.rollout.clusterHealthTimeout` | duration | yes | timeout for cluster health gates during rollout |
| `spec.hibernation.offloadTimeout` | duration | yes | timeout for offload or disconnect before scale-down |
| `spec.safety.requireClusterHealthy` | bool | yes | require cluster health before restart sequencing |
| `spec.autoscaling.mode` | enum `Disabled|Advisory|Enforced` | no | controller-owned autoscaling intent |
| `spec.autoscaling.scaleUp.enabled` | bool | no | allow one-step controller-owned scale-up |
| `spec.autoscaling.scaleUp.cooldown` | duration | no | minimum time between controller-owned scale-up actions |
| `spec.autoscaling.scaleDown.enabled` | bool | no | allow one-step controller-owned scale-down |
| `spec.autoscaling.scaleDown.cooldown` | duration | no | minimum time between controller-owned scale-down actions |
| `spec.autoscaling.scaleDown.stabilizationWindow` | duration | no | minimum stable low-pressure window before a scale-down step |
| `spec.autoscaling.minReplicas` | integer | no | minimum advisory or enforced target size |
| `spec.autoscaling.maxReplicas` | integer | no | maximum advisory or enforced target size |
| `spec.autoscaling.signals[]` | enum list | no | enabled autoscaling signals |

### `spec.restartPolicy.tlsDrift`

Recommended enum values:

- `AutoreloadThenRestartOnFailure`
- `AlwaysRestart`
- `ObserveOnly`

Default for MVP:

- `AutoreloadThenRestartOnFailure`

## Status Schema

| Field | Type | Purpose |
| --- | --- | --- |
| `status.observedGeneration` | integer | last reconciled resource generation |
| `status.observedStatefulSetRevision` | string | last observed desired workload revision |
| `status.observedConfigHash` | string | aggregate hash for watched config state |
| `status.observedCertificateHash` | string | aggregate hash for watched TLS state |
| `status.observedTLSConfigurationHash` | string | last reconciled TLS wiring fingerprint from the target StatefulSet |
| `status.tls.observationStartedAt` | timestamp | start of the TLS autoreload observation window |
| `status.tls.targetCertificateHash` | string | TLS content hash currently under observation |
| `status.tls.targetTLSConfigurationHash` | string | TLS wiring fingerprint currently under observation |
| `status.rollout.trigger` | enum | rollout source currently in progress |
| `status.rollout.startedAt` | timestamp | durable marker used to resume a config-triggered rollout |
| `status.rollout.targetConfigHash` | string | config hash the current rollout is applying |
| `status.rollout.targetCertificateHash` | string | TLS content hash the current rollout is applying |
| `status.rollout.targetTLSConfigurationHash` | string | TLS wiring fingerprint the current rollout is applying |
| `status.replicas.desired` | integer | current desired replicas on the target workload |
| `status.replicas.ready` | integer | current ready pods |
| `status.replicas.updated` | integer | pods at the desired revision |
| `status.clusterNodes.connected` | integer | NiFi nodes connected to the cluster |
| `status.clusterNodes.disconnected` | integer | NiFi nodes disconnected from the cluster |
| `status.clusterNodes.offloaded` | integer | NiFi nodes explicitly offloaded |
| `status.hibernation.lastRunningReplicas` | integer | last non-zero running size used for restore |
| `status.hibernation.baselineReplicas` | integer | preserved non-zero running target used if the last running size is unavailable |
| `status.autoscaling.recommendedReplicas` | integer | latest advisory or enforced autoscaling recommendation |
| `status.autoscaling.reason` | string | current autoscaling recommendation or block reason |
| `status.autoscaling.signals[]` | list | per-signal availability and current summary |
| `status.autoscaling.lastEvaluationTime` | timestamp | last autoscaling evaluation that changed recommendation meaning |
| `status.autoscaling.lowPressureSince` | timestamp | backward-compatible low-pressure checkpoint used for scale-down stabilization |
| `status.autoscaling.lowPressure.*` | struct | durable low-pressure evidence, including consecutive zero-backlog samples |
| `status.autoscaling.lastScalingDecision` | string | latest controller decision or block message |
| `status.autoscaling.lastScaleUpTime` | timestamp | last successful controller-owned scale-up time |
| `status.autoscaling.lastScaleDownTime` | timestamp | last successful controller-owned scale-down time |
| `status.autoscaling.execution.phase` | enum | persisted scale-up settle or scale-down prepare or settle checkpoint |
| `status.autoscaling.execution.state` | enum `Running|Blocked|Failed` | explicit execution state for resume and diagnostics |
| `status.autoscaling.execution.lastTransitionTime` | timestamp | last blocked, resumed, or failed execution transition |
| `status.autoscaling.execution.message` | string | current autoscaling execution summary |
| `status.autoscaling.execution.blockedReason` | string | explicit blocked reason when execution is waiting or timed out |
| `status.autoscaling.execution.failureReason` | string | explicit failure reason when execution stops |
| `status.nodeOperation` | struct | persisted NiFi disconnect or offload step for restart or hibernation |
| `status.lastOperation` | struct | current or last lifecycle action summary |
| `status.conditions[]` | `metav1.Condition`-style list | machine-readable health and progress conditions |

## Conditions

| Condition | Meaning |
| --- | --- |
| `TargetResolved` | the referenced target exists and is valid |
| `Available` | the desired state is satisfied and cluster health is acceptable |
| `Progressing` | the controller is actively rolling, hibernating, restoring, or converging |
| `Degraded` | the controller is blocked or the cluster is not meeting safety expectations |
| `Hibernated` | the cluster has been intentionally reduced to zero replicas |

## Example YAMLs

### Minimal Managed Cluster

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: nifi
  namespace: nifi
spec:
  targetRef:
    name: nifi
  desiredState: Running
  suspend: false
  restartTriggers:
    configMaps:
    - name: nifi-config
    secrets:
    - name: nifi-tls
  restartPolicy:
    tlsDrift: AutoreloadThenRestartOnFailure
  rollout:
    minReadySeconds: 30
    podReadyTimeout: 10m
    clusterHealthTimeout: 15m
  hibernation:
    offloadTimeout: 5m
  safety:
    requireClusterHealthy: true
```

### Managed Cluster With Explicit TLS Restart Policy

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: nifi
  namespace: nifi
spec:
  targetRef:
    name: nifi
  desiredState: Running
  suspend: false
  restartTriggers:
    configMaps:
    - name: nifi-config
    - name: login-identity-providers
    secrets:
    - name: nifi-tls
    - name: nifi-ldap-bind
  restartPolicy:
    tlsDrift: AlwaysRestart
  rollout:
    minReadySeconds: 60
    podReadyTimeout: 15m
    clusterHealthTimeout: 20m
  hibernation:
    offloadTimeout: 10m
  safety:
    requireClusterHealthy: true
```

### Hibernated Cluster

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiCluster
metadata:
  name: nifi
  namespace: nifi
spec:
  targetRef:
    name: nifi
  desiredState: Hibernated
  suspend: false
  restartTriggers:
    configMaps:
    - name: nifi-config
    secrets:
    - name: nifi-tls
  restartPolicy:
    tlsDrift: AutoreloadThenRestartOnFailure
  rollout:
    minReadySeconds: 30
    podReadyTimeout: 10m
    clusterHealthTimeout: 15m
  hibernation:
    offloadTimeout: 5m
  safety:
    requireClusterHealthy: true
status:
  hibernation:
    lastRunningReplicas: 3
  conditions:
  - type: Hibernated
    status: "True"
    reason: Hibernated
    message: Cluster is hibernated and ready for restore
```

## Field-By-Field Rationale

### Why `targetRef.name` Exists

The controller must know which chart-managed workload it should watch and orchestrate. `targetRef.name` provides that link without turning the CRD into a workload template.

`v1alpha1` intentionally fixes the target kind to `StatefulSet`:

- it matches the expected NiFi deployment shape
- it removes unnecessary API surface
- it leaves room for future generalization if a real need appears

### Why Hibernation State Needs Prior Replica Memory

Hibernation is not safe if restore depends on a guessed replica count. `status.hibernation.lastRunningReplicas` gives the controller a durable restore target for the most recent running size, while `status.hibernation.baselineReplicas` preserves the non-zero intended shape if restore happens after status loss or an interrupted transition. The controller falls back to `1` only when both status hints are absent.

The current implementation uses one explicit fallback when that field is absent:

- restore to `1` replica

That fallback is intentionally simple. It is only there to recover cleanly from older status or manual state, not to replace explicit prior replica tracking.

### Why `status.nodeOperation` Exists

Disconnect and offload are live NiFi operations, not template drift. The controller needs one durable place to remember which pod and NiFi node it is currently preparing so a restart does not skip the safety step or repeat the wrong destructive action.

### Why Autoscaling Status Carries Low-Pressure Evidence And Execution State

Autoscaling stays inside the same controller-owned lifecycle plane.

- `status.autoscaling.lowPressure.*` records the strongest durable low-pressure signal this platform can currently prove: repeated zero-backlog observations from NiFi runtime APIs.
- `status.autoscaling.execution.*` records whether autoscaling is running, blocked, or failed, so restart recovery and operator debugging do not depend on transient logs or generic condition text.
- `status.nodeOperation` and `status.autoscaling.execution` are separate on purpose: one tracks the NiFi disconnect or offload checkpoint, the other tracks the autoscaling lifecycle checkpoint that owns replica changes.

### Why Rollout And Safety Knobs Are Small And Typed

The controller needs a few operational settings, but not an entire values tree. Small typed fields are easier to validate, document, and test than a generic values blob.

### How Watched Inputs Are Classified

The watched-input model stays intentionally small:

- every `spec.restartTriggers.configMaps[]` entry contributes to config drift
- a watched Secret contributes to certificate drift only when it matches the TLS Secret mounted by the target StatefulSet
- every other watched Secret contributes to config drift

This keeps the API thin while still separating general restart-trigger inputs from TLS-specific policy handling.

### TLS Decision Fields

The TLS status fields exist for one reason: they let the controller resume policy-driven TLS handling safely after a restart.

- `status.observedCertificateHash` changes only when the controller considers TLS content reconciled
- `status.observedTLSConfigurationHash` changes only when the controller considers TLS wiring reconciled
- `status.tls.*` persists the autoreload observation window
- `status.rollout.targetCertificateHash` and `status.rollout.targetTLSConfigurationHash` persist restart-required TLS rollout targets

## Intentionally Omitted Fields

These are omitted on purpose:

- inline Helm values
- ingress and Route schemas
- storage class and PVC sizing schemas
- resource requests and limits schemas
- authentication provider schemas
- flow deployment configuration
- user, group, and policy configuration
- NiFi Registry configuration
- per-node pool definitions
- backup policy fields
- generic arbitrary values maps

Those concerns belong in Helm values, later design work, or not in this project at all.
