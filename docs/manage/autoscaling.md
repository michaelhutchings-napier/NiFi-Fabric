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

## Optional KEDA Integration

KEDA is optional and experimental.

KEDA does not scale the NiFi `StatefulSet` directly. It writes external intent through `NiFiCluster`, and the controller decides whether that intent becomes a real scale action.

See [Experimental Features](../experimental-features.md) and [KEDA Integration Position](../keda.md).
