# Autoscaling

NiFi-Fabric uses a controller-owned autoscaling model.

The controller is the only component that makes real scale changes in managed mode.

## Autoscaling Modes

- `Disabled`
- `Advisory`
- `Enforced`

## What It Does

NiFi-Fabric can:

- recommend scale-up or scale-down actions
- perform scale-up
- perform safe, one-node-at-a-time scale-down
- respect higher-priority lifecycle work such as rollout, TLS handling, hibernation, and restore

## KEDA

KEDA is optional.

When enabled:

- KEDA writes intent through `NiFiCluster`
- the controller still decides whether a safe action should happen
- KEDA does not take direct ownership of the NiFi `StatefulSet`

## Configuration

Use:

- `cluster.autoscaling.*` in the platform chart
- `NiFiCluster.spec.autoscaling.*` in managed mode

## Support Position

NiFi-Fabric keeps autoscaling conservative and explainable:

- advisory recommendations are supported
- enforced scale-up is supported
- enforced scale-down stays conservative and one node at a time

For detailed support reading, see [Compatibility](../compatibility.md) and [Verification and Support Levels](../testing.md).
