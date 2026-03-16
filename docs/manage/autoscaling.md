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
- highest ordinal is removed first
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

## Optional KEDA Integration

KEDA is optional and experimental.

KEDA does not scale the NiFi `StatefulSet` directly. It writes external intent through `NiFiCluster`, and the controller decides whether that intent becomes a real scale action.

See [Experimental Features](../experimental-features.md) and [KEDA Integration Position](../keda.md).
