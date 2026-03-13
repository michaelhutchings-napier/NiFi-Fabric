# NiFiCluster Reference

`NiFiCluster` is the thin operational API used by the managed platform chart.

- API version: `platform.nifi.io/v1alpha1`
- scope: namespaced
- installed by: `charts/nifi-platform` in managed mode

## Spec

### Core fields

| Field | Type | Notes |
| --- | --- | --- |
| `spec.targetRef.name` | string | Required. Name of the chart-managed `StatefulSet` in the same namespace. |
| `spec.desiredState` | `Running` or `Hibernated` | Required. Desired high-level runtime state. |
| `spec.suspend` | bool | Pause active reconciliation while keeping the resource. |

### Restart triggers and policy

| Field | Type | Notes |
| --- | --- | --- |
| `spec.restartTriggers.configMaps[]` | object reference list | ConfigMaps observed for restart or rollout decisions. |
| `spec.restartTriggers.secrets[]` | object reference list | Secrets observed for restart or rollout decisions. |
| `spec.restartPolicy.tlsDrift` | enum | `AutoreloadThenRestartOnFailure`, `AlwaysRestart`, or `ObserveOnly`. |

### Rollout

| Field | Type | Notes |
| --- | --- | --- |
| `spec.rollout.minReadySeconds` | int32 | Minimum ready time before rollout advancement. |
| `spec.rollout.podReadyTimeout` | duration | Per-pod timeout during rollout and convergence. |
| `spec.rollout.clusterHealthTimeout` | duration | Cluster-level health timeout for managed lifecycle steps. |

### Hibernation

| Field | Type | Notes |
| --- | --- | --- |
| `spec.hibernation.offloadTimeout` | duration | Time allowed for NiFi offload during destructive steps. |

### Safety

| Field | Type | Notes |
| --- | --- | --- |
| `spec.safety.requireClusterHealthy` | bool | Require cluster health before managed destructive steps. |

### Autoscaling

| Field | Type | Notes |
| --- | --- | --- |
| `spec.autoscaling.mode` | `Disabled`, `Advisory`, `Enforced` | Controller-owned autoscaling mode. |
| `spec.autoscaling.scaleUp.enabled` | bool | Enables controller-owned scale-up in enforced mode. |
| `spec.autoscaling.scaleUp.cooldown` | duration | Minimum time between scale-up actions. |
| `spec.autoscaling.scaleDown.enabled` | bool | Enables controller-owned safe scale-down in enforced mode. |
| `spec.autoscaling.scaleDown.cooldown` | duration | Minimum time between scale-down actions. |
| `spec.autoscaling.scaleDown.stabilizationWindow` | duration | Required low-pressure stability window before scale-down. |
| `spec.autoscaling.external.enabled` | bool | Enables the external intent surface used by optional KEDA integration. |
| `spec.autoscaling.external.source` | enum | Current external source enum includes `KEDA`. |
| `spec.autoscaling.external.scaleDownEnabled` | bool | Allows best-effort external downscale intent to be considered by the controller. |
| `spec.autoscaling.external.requestedReplicas` | int32 | Controller-owned external requested replicas field; also backs the `/scale` subresource. |
| `spec.autoscaling.minReplicas` | int32 | Lower bound for autoscaling recommendations and execution. |
| `spec.autoscaling.maxReplicas` | int32 | Upper bound for autoscaling recommendations and execution. |
| `spec.autoscaling.signals[]` | enum list | Current signals include `QueuePressure` and `CPU`. |

## Status

### Observed state

| Field | Type | Notes |
| --- | --- | --- |
| `status.observedGeneration` | int64 | Last reconciled resource generation. |
| `status.observedStatefulSetRevision` | string | Last observed workload revision. |
| `status.observedConfigHash` | string | Last observed aggregate config hash. |
| `status.observedCertificateHash` | string | Last observed TLS content hash. |
| `status.observedTLSConfigurationHash` | string | Last observed TLS wiring hash. |

### TLS and rollout

| Field | Type | Notes |
| --- | --- | --- |
| `status.tls.*` | struct | Current TLS observation window state. |
| `status.rollout.*` | struct | Current or last rollout trigger and target state. |

### Cluster state

| Field | Type | Notes |
| --- | --- | --- |
| `status.replicas.desired` | int32 | Current desired replicas on the target. |
| `status.replicas.ready` | int32 | Ready pod count. |
| `status.replicas.updated` | int32 | Updated pod count. |
| `status.scaleSelector` | string | Selector exposed for the `/scale` subresource. |
| `status.clusterNodes.connected` | int32 | Connected NiFi nodes. |
| `status.clusterNodes.disconnected` | int32 | Disconnected NiFi nodes. |
| `status.clusterNodes.offloaded` | int32 | Offloaded NiFi nodes. |
| `status.hibernation.lastRunningReplicas` | int32 | Last non-zero running size used for restore. |
| `status.hibernation.baselineReplicas` | int32 | Preserved fallback running size. |

### Autoscaling status

| Field | Type | Notes |
| --- | --- | --- |
| `status.autoscaling.recommendedReplicas` | int32 pointer | Latest controller recommendation. |
| `status.autoscaling.reason` | string | Current recommendation or block reason. |
| `status.autoscaling.signals[]` | list | Per-signal availability and summary. |
| `status.autoscaling.lastEvaluationTime` | timestamp | Last meaningful autoscaling evaluation. |
| `status.autoscaling.lowPressureSince` | timestamp | Compatibility field for low-pressure tracking. |
| `status.autoscaling.lowPressure.*` | struct | Durable low-pressure evidence. |
| `status.autoscaling.lastScalingDecision` | string | Latest execution or block message. |
| `status.autoscaling.lastScaleUpTime` | timestamp | Last successful scale-up time. |
| `status.autoscaling.lastScaleDownTime` | timestamp | Last successful scale-down time. |
| `status.autoscaling.execution.*` | struct | Durable execution checkpoint, state, blocked reason, and failure reason. |
| `status.autoscaling.external.*` | struct | External intent observed, actionable, ignored, and source information. |

### Operation state

| Field | Type | Notes |
| --- | --- | --- |
| `status.nodeOperation.*` | struct | Current disconnect or offload operation. |
| `status.lastOperation.*` | struct | Current or last lifecycle operation summary. |
| `status.conditions[]` | condition list | Machine-readable lifecycle state. |

## Conditions

Current condition types:

- `TargetResolved`
- `Available`
- `Progressing`
- `Degraded`
- `Hibernated`

## Scale Subresource

`NiFiCluster` exposes Kubernetes `/scale`.

Important behavior:

- `/scale` writes `spec.autoscaling.external.requestedReplicas`
- `/scale` reads back through `status.replicas.desired`
- this exists to support controller-mediated external intent, including optional KEDA integration
- the controller still remains the only executor that mutates the NiFi `StatefulSet`
