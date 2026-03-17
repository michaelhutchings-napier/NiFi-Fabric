# Autoscaling

NiFi-Fabric uses a controller-owned autoscaling model.

## What This Feature Does

The `NiFiCluster` controller is the only executor of replica changes.

Autoscaling runs in three modes:

- `Disabled`
- `Advisory`
- `Enforced`

The controller remains responsible for:

- scale-up execution
- scale-down safety checks
- disconnect, offload, and delete sequencing
- lifecycle precedence around rollout, TLS actions, hibernation, restore, degraded states, and active destructive work

## Standard Configuration Surface

Use `NiFiCluster.spec.autoscaling` or platform chart values under:

- `cluster.autoscaling.mode`
- `cluster.autoscaling.scaleUp.*`
- `cluster.autoscaling.scaleDown.*`
- `cluster.autoscaling.minReplicas`
- `cluster.autoscaling.maxReplicas`
- `cluster.autoscaling.signals`

## Support Level

- advisory recommendations: primary supported model
- enforced scale-up: primary supported controller-owned execution path
- enforced scale-down: available, but intentionally conservative and experimental

## Scale-Down Position

Scale-down remains one-step-at-a-time.

That is intentional:

- NiFi node removal is destructive
- highest ordinal is removed first because StatefulSet semantics make it the only bounded one-step removal candidate in this model
- disconnect and offload must complete before deletion
- bulk scale-down is not supported

Low-pressure evidence is also intentionally conservative.

Scale-down is only eligible when the controller has durable evidence that the cluster is genuinely quiet:

- root-process-group backlog must stay at zero across repeated evaluations
- when NiFi thread counts are available, active timer-driven work must also stay below the low-pressure threshold
- when byte backlog or thread evidence is missing, the controller requires extra consecutive qualifying samples before any removal step
- stabilization and cooldown windows still apply even after low pressure qualifies

This keeps the policy explainable:

- scale-down is allowed only after repeated trustworthy low-pressure evidence
- scale-down is blocked explicitly when zero backlog appears transient or executor activity is still too busy to trust that quiet sample

## Reading Autoscaling State

Operators should read autoscaling from the existing `NiFiCluster` surfaces together:

- `spec.autoscaling.mode` shows whether the cluster is in `Disabled`, `Advisory`, or `Enforced` mode.
- `status.autoscaling.external.requestedReplicas` shows the latest external request the controller observed.
- `status.autoscaling.recommendedReplicas` shows the controller's bounded recommendation after policy evaluation.
- `status.autoscaling.execution.phase`, `state`, `blockedReason`, `failureReason`, and `message` show the live execution checkpoint when a scale action is settling or blocked.
- `status.autoscaling.lastScalingDecision` is the operator-facing summary for allowed, blocked, deferred, ignored, or failed decisions and now appends compact context for mode, current replicas, recommendation, request, and active execution.
- `status.nodeOperation` identifies the pod and stage being disconnected or offloaded during safe scale-down.

Typical operator interpretation:

- if `external.requestedReplicas` differs from `recommendedReplicas`, policy or safety checks have bounded or ignored the external request
- if `recommendedReplicas` differs from the current desired size but `execution` is empty, the controller is still blocked or deferred by cooldown, stabilization, lifecycle precedence, or availability gates
- if `execution.state=Blocked`, the controller will either resume automatically on the next safe reconcile or the message will tell you what operator action is needed

## Optional KEDA Integration

KEDA is optional and experimental.

KEDA does not scale the NiFi `StatefulSet` directly. It writes external intent through `NiFiCluster`, and the controller decides whether that intent becomes a real scale action.

See [Experimental Features](../experimental-features.md) and [KEDA Integration Position](../keda.md).
