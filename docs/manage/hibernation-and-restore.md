# Hibernation and Restore

NiFi-Fabric supports managed hibernation and restore through `NiFiCluster`.

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
- repository runtime verification: available on kind
