# Hibernation and Restore

NiFi-Fabric supports managed hibernation and restore through `NiFiCluster`.

This is a lifecycle-control feature, not a backup or disaster-recovery feature.
Use it to scale a cluster safely to zero and back again. Do not treat it as a
replacement for control-plane export, secret escrow, or PVC snapshot recovery.

## What This Feature Does

- hibernation reduces the cluster to zero replicas
- restore returns the cluster to the last running size
- the controller keeps lifecycle ownership of the scale-to-zero and restore sequence

## Configuration Surface

Use:

- `NiFiCluster.spec.desiredState`
- `NiFiCluster.spec.hibernation.offloadTimeout`

In the platform chart, these map to:

- `cluster.desiredState`
- `cluster.hibernation.offloadTimeout`

## Safety Model

Before removing a node, the controller reuses the same safe sequencing principles used elsewhere:

- lifecycle precedence stays in force
- highest ordinal is removed first
- NiFi disconnect and offload sequencing is reused for destructive steps
- restore waits for health and convergence

## Support Level

- hibernation: supported
- restore: supported
- local kind coverage is available for this feature

For backup and recovery boundaries, see [Backup, Restore, and Disaster Recovery](../dr.md).
