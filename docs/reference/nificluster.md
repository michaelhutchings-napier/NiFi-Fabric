# NiFiCluster Reference

`NiFiCluster` is the managed operational API used by `charts/nifi-platform`.

- API version: `platform.nifi.io/v1alpha1`
- Scope: namespaced
- Installed by: `charts/nifi-platform` in managed mode

For lifecycle behavior, see [Hibernation and Restore](../manage/hibernation-and-restore.md), [Autoscaling](../manage/autoscaling.md), and [Architecture Summary](../architecture.md).

Defaults in this page are shown only when they are real API defaults or fixed enum values. Many practical defaults come from the platform chart or controller behavior and are intentionally left blank here.

## NiFiCluster

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `apiVersion` | string | Kubernetes API version. | Yes | `platform.nifi.io/v1alpha1` |
| `kind` | string | Resource kind. | Yes | `NiFiCluster` |
| `metadata.name` | string | Resource name. | Yes |  |
| `spec` | object | Desired managed state. | Yes |  |
| `status` | object | Observed state reported by the controller. | No |  |

## NiFiClusterSpec

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.targetRef` | object | Managed workload target. | Yes |  |
| `spec.desiredState` | enum | High-level runtime intent. Values: `Running`, `Hibernated`. | Yes |  |
| `spec.suspend` | boolean | Pauses active reconciliation. | No |  |
| `spec.restartTriggers` | object | ConfigMaps and Secrets watched for restart decisions. | No |  |
| `spec.restartPolicy` | object | Restart behavior for TLS drift. | No |  |
| `spec.rollout` | object | Managed rollout timing settings. | No |  |
| `spec.hibernation` | object | Hibernation timing settings. | No |  |
| `spec.safety` | object | Safety gates for destructive actions. | No |  |
| `spec.autoscaling` | object | Controller-owned autoscaling policy. | No |  |

## TargetRef

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.targetRef.name` | string | Name of the chart-managed `StatefulSet` in the same namespace. | Yes |  |

## RestartTriggers

The platform chart wires chart-owned config surfaces into `spec.restartTriggers` only when those features need a restart-aware reconcile path. In the current bounded model, both `parameterContexts` and `versionedFlowImports` reconcile live in-pod on pod `-0` and are intentionally not wired into `spec.restartTriggers`.

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.restartTriggers.configMaps[]` | `LocalObjectReference` | ConfigMaps observed for restart or rollout decisions. | No |  |
| `spec.restartTriggers.secrets[]` | `LocalObjectReference` | Secrets observed for restart or rollout decisions. | No |  |

## RestartPolicy

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.restartPolicy.tlsDrift` | enum | TLS drift behavior. Values: `AutoreloadThenRestartOnFailure`, `AlwaysRestart`, `ObserveOnly`. | No |  |

## Rollout

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.rollout.minReadySeconds` | integer | Minimum ready time before rollout advancement. | No |  |
| `spec.rollout.podReadyTimeout` | duration | Per-pod ready timeout during managed rollout. | No |  |
| `spec.rollout.clusterHealthTimeout` | duration | Cluster-level health timeout for rollout and settle checks. | No |  |

## Hibernation

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.hibernation.offloadTimeout` | duration | Maximum time allowed for NiFi offload before destructive steps continue or fail. | No |  |

## Safety

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.safety.requireClusterHealthy` | boolean | Requires a healthy cluster before destructive managed actions. | No |  |

## Autoscaling

The current supported built-in autoscaling model is the bounded controller-owned production path:

- `Advisory` remains the production-ready recommendation path
- `Enforced` remains the production-ready execution path for bounded scale-up and bounded sequential one-node scale-down work
- the richer built-in policy depth is part of that support claim, including confidence-based scale-up, bounded capacity reasoning, actual StatefulSet removal-candidate qualification, and sequential multi-step scale-down episodes with fresh requalification between steps
- optional KEDA external intent is a supported bounded integration layered onto this autoscaling surface

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.autoscaling.mode` | enum | Autoscaling mode. Values: `Disabled`, `Advisory`, `Enforced`. | No |  |
| `spec.autoscaling.scaleUp` | object | Enforced scale-up settings. | No |  |
| `spec.autoscaling.scaleDown` | object | Controller-owned safe scale-down settings for the bounded execution path. The controller still removes only one pod at a time, even when a bounded sequential episode plans more than one removal, and qualifies the actual StatefulSet removal pod before destructive work starts. | No |  |
| `spec.autoscaling.external` | object | External intent surface used by optional KEDA integration. | No |  |
| `spec.autoscaling.minReplicas` | integer | Lower bound for controller recommendations and execution. | No |  |
| `spec.autoscaling.maxReplicas` | integer | Upper bound for controller recommendations and execution. | No |  |
| `spec.autoscaling.signals[]` | enum list | Signals the controller may evaluate. Current values: `QueuePressure`, `CPU`. | No |  |

## AutoscalingScaleUp

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.autoscaling.scaleUp.enabled` | boolean | Enables controller-owned scale-up when `mode=Enforced`. | No |  |
| `spec.autoscaling.scaleUp.cooldown` | duration | Minimum time between successful scale-up actions. | No |  |

## AutoscalingScaleDown

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.autoscaling.scaleDown.enabled` | boolean | Enables controller-owned safe scale-down when `mode=Enforced`. | No |  |
| `spec.autoscaling.scaleDown.cooldown` | duration | Minimum time between successful scale-down actions. | No |  |
| `spec.autoscaling.scaleDown.stabilizationWindow` | duration | Required low-pressure stability window before scale-down is allowed. | No |  |
| `spec.autoscaling.scaleDown.maxSequentialSteps` | integer | Maximum number of one-node removals the controller may complete in one bounded sequential scale-down episode. | No |  |

## AutoscalingExternal

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `spec.autoscaling.external.enabled` | boolean | Enables the external intent surface. | No |  |
| `spec.autoscaling.external.source` | enum | External source name. Current value: `KEDA`. Optional supported external intent input path. | No |  |
| `spec.autoscaling.external.scaleDownEnabled` | boolean | Allows best-effort external downscale intent to be considered by the controller through the existing bounded safe scale-down path. Optional supported external intent input path. | No |  |
| `spec.autoscaling.external.requestedReplicas` | integer | External requested replica count. Also backs the Kubernetes `/scale` subresource. | No |  |

## NiFiClusterStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.observedGeneration` | integer | Last reconciled resource generation. | No |  |
| `status.observedStatefulSetRevision` | string | Last observed workload revision. | No |  |
| `status.observedConfigHash` | string | Last observed aggregate config hash. | No |  |
| `status.observedCertificateHash` | string | Last observed TLS content hash. | No |  |
| `status.observedTLSConfigurationHash` | string | Last observed TLS wiring hash. | No |  |
| `status.tls` | object | Current TLS observation window. | No |  |
| `status.rollout` | object | Current or last rollout target state. | No |  |
| `status.replicas` | object | Desired, ready, and updated replica counts. | No |  |
| `status.scaleSelector` | string | Label selector exposed for `/scale`. | No |  |
| `status.clusterNodes` | object | Connected, disconnected, and offloaded NiFi node counts. | No |  |
| `status.hibernation` | object | Stored replica counts used for restore. | No |  |
| `status.autoscaling` | object | Latest autoscaling recommendation, execution state, and external intent status. | No |  |
| `status.nodeOperation` | object | Current disconnect or offload operation. | No |  |
| `status.lastOperation` | object | Current or last lifecycle operation summary. | No |  |
| `status.conditions[]` | `Condition` list | Machine-readable lifecycle conditions. | No |  |

## TLSStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.tls.observationStartedAt` | timestamp | When the current TLS observation window began. | No |  |
| `status.tls.targetCertificateHash` | string | Certificate hash the controller is evaluating or has settled on. | No |  |
| `status.tls.targetTLSConfigurationHash` | string | TLS configuration hash the controller is evaluating or has settled on. | No |  |

## RolloutStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.rollout.trigger` | enum | Rollout trigger. Values: `StatefulSetRevision`, `ConfigDrift`, `TLSDrift`. | No |  |
| `status.rollout.startedAt` | timestamp | When the current rollout began. | No |  |
| `status.rollout.targetRevision` | string | Target StatefulSet revision. | No |  |
| `status.rollout.targetConfigHash` | string | Target config hash for config-drift rollout. | No |  |
| `status.rollout.targetCertificateHash` | string | Target certificate hash for TLS-drift rollout. | No |  |
| `status.rollout.targetTLSConfigurationHash` | string | Target TLS wiring hash for TLS-drift rollout. | No |  |
| `status.rollout.completedPods[]` | string list | Pods already completed in the current rollout. | No |  |

## ReplicaStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.replicas.desired` | integer | Current desired replicas on the managed target. | No |  |
| `status.replicas.ready` | integer | Ready pod count. | No |  |
| `status.replicas.updated` | integer | Updated pod count. | No |  |

## ClusterNodesStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.clusterNodes.connected` | integer | Connected NiFi nodes. | No |  |
| `status.clusterNodes.disconnected` | integer | Disconnected NiFi nodes. | No |  |
| `status.clusterNodes.offloaded` | integer | Offloaded NiFi nodes. | No |  |

## HibernationStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.hibernation.lastRunningReplicas` | integer | Last non-zero running size observed before hibernation. | No |  |
| `status.hibernation.baselineReplicas` | integer | Stored fallback size used if a restore baseline is needed. | No |  |

## AutoscalingStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.autoscaling.recommendedReplicas` | integer pointer | Latest controller recommendation. | No |  |
| `status.autoscaling.reason` | string | Current recommendation, block, or evaluation reason. | No |  |
| `status.autoscaling.signals[]` | object list | Per-signal availability and summary. | No |  |
| `status.autoscaling.lastEvaluationTime` | timestamp | Last meaningful autoscaling evaluation time. | No |  |
| `status.autoscaling.lowPressureSince` | timestamp | Compatibility field for low-pressure tracking. | No |  |
| `status.autoscaling.lowPressure` | object | Durable low-pressure evidence used for safe scale-down. | No |  |
| `status.autoscaling.lastScalingDecision` | string | Latest execution, block, defer, ignore, or failure summary. The message is operator-facing and may append compact mode, request, recommendation, execution context, sequential episode progress, scale-down candidate selection or rejection detail, and bounded capacity-planning context such as whether pressure is still building or current capacity appears tight. | No |  |
| `status.autoscaling.lastScaleUpTime` | timestamp | Last successful scale-up time. | No |  |
| `status.autoscaling.lastScaleDownTime` | timestamp | Last successful scale-down time. | No |  |
| `status.autoscaling.execution` | object | Durable execution phase, blocked reason, or failure reason. | No |  |
| `status.autoscaling.external` | object | Observed external intent, controller-bounded intent, and current handling state. | No |  |

## AutoscalingExecutionStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.autoscaling.execution.phase` | enum | Current execution checkpoint. Values: `ScaleUpSettle`, `ScaleDownPrepare`, `ScaleDownSettle`. | No |  |
| `status.autoscaling.execution.state` | enum | Current execution state. Values: `Running`, `Blocked`, `Failed`. | No |  |
| `status.autoscaling.execution.startedAt` | timestamp | When the current autoscaling execution started. | No |  |
| `status.autoscaling.execution.lastTransitionTime` | timestamp | Last state transition time. | No |  |
| `status.autoscaling.execution.targetReplicas` | integer pointer | Replica target for the current execution. | No |  |
| `status.autoscaling.execution.plannedSteps` | integer | Number of one-node removals currently planned in the active bounded sequential scale-down episode. | No |  |
| `status.autoscaling.execution.completedSteps` | integer | Number of one-node removals already completed in the active bounded sequential scale-down episode. | No |  |
| `status.autoscaling.execution.message` | string | Human-readable execution summary for the current settle or block checkpoint, including selected-candidate or rejected-candidate reasoning and sequential episode progress during scale-down. | No |  |
| `status.autoscaling.execution.blockedReason` | string | Short blocked reason when execution is blocked, including scale-down candidate reasons such as missing, terminating, or not-Ready removal candidates, plus bounded between-step reasons such as cooldown or stabilization pending. | No |  |
| `status.autoscaling.execution.failureReason` | string | Short failure reason when execution fails. | No |  |

## AutoscalingExternalStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.autoscaling.external.observed` | boolean | Whether the controller observed external intent. | No |  |
| `status.autoscaling.external.source` | enum | Observed external source. Current value: `KEDA`. | No |  |
| `status.autoscaling.external.requestedReplicas` | integer pointer | Last observed external requested replicas. | No |  |
| `status.autoscaling.external.boundedReplicas` | integer pointer | Controller-bounded external intent after autoscaling min and max checks. | No |  |
| `status.autoscaling.external.actionable` | boolean | Whether the external request is currently actionable instead of deferred or blocked. | No |  |
| `status.autoscaling.external.scaleDownIgnored` | boolean | Whether an external scale-down request was ignored. | No |  |
| `status.autoscaling.external.reason` | string | Short reason for current external intent handling, such as actionable, cooldown waiting, low-pressure waiting, lifecycle-blocked, or ignored. | No |  |
| `status.autoscaling.external.message` | string | Human-readable summary for the external request, including the raw request, any controller bounds, and why execution is active, deferred, blocked, or ignored. | No |  |

## NodeOperationStatus

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.nodeOperation.purpose` | enum | Current operation purpose. Values: `Restart`, `Hibernation`, `ScaleDown`. | No |  |
| `status.nodeOperation.podName` | string | Pod currently being disconnected or offloaded. | No |  |
| `status.nodeOperation.podUid` | string | Pod UID currently being acted on. | No |  |
| `status.nodeOperation.nodeId` | string | NiFi node ID currently being acted on. | No |  |
| `status.nodeOperation.stage` | enum | Current stage. Values: `Disconnecting`, `Offloading`. | No |  |
| `status.nodeOperation.startedAt` | timestamp | When the current node operation started. | No |  |

## LastOperation

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.lastOperation.type` | string | Lifecycle operation type. | No |  |
| `status.lastOperation.phase` | enum | Operation phase. Values: `Pending`, `Running`, `Succeeded`, `Failed`. | No |  |
| `status.lastOperation.startedAt` | timestamp | When the operation started. | No |  |
| `status.lastOperation.completedAt` | timestamp | When the operation completed. | No |  |
| `status.lastOperation.message` | string | Human-readable operation summary. | No |  |

## Conditions

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `status.conditions[].type` | string | Condition type. Current controller condition types include `TargetResolved`, `Available`, `Progressing`, `Degraded`, and `Hibernated`. | No |  |
| `status.conditions[].status` | enum | Kubernetes condition status. Values: `True`, `False`, `Unknown`. | No |  |
| `status.conditions[].reason` | string | Short machine-readable reason. | No |  |
| `status.conditions[].message` | string | Human-readable condition message. | No |  |

## Scale Subresource

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `/scale spec` | integer | Writes `spec.autoscaling.external.requestedReplicas`. | No |  |
| `/scale status` | integer | Reads back through `status.replicas.desired`. | No |  |
| `/scale selector` | string | Reads back through `status.scaleSelector`. | No |  |

The `/scale` subresource exists to support controller-mediated external intent, including optional KEDA integration. The controller still remains the only executor that mutates the NiFi `StatefulSet`.
