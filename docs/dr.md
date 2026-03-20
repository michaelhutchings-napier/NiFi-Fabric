# Backup, Restore, and Disaster Recovery

NiFi-Fabric treats backup and recovery as an operator responsibility built on a clear split:

- the platform owns declarative install and lifecycle intent
- NiFi owns NiFi runtime behavior
- the storage platform owns PVC snapshot and restore behavior

## The Two-Layer Model

Think about recovery in two layers:

1. control-plane backup
2. data-plane recovery

## Control-Plane Backup

Control-plane backup means preserving the declarative platform intent, including:

- Helm values and overlays
- rendered manifest intent when bundle workflows are used
- the managed `NiFiCluster` resource
- references to Secrets, ConfigMaps, issuers, and other prerequisites

NiFi-Fabric includes a thin export helper for this layer:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

## Data-Plane Recovery

Data-plane recovery means restoring NiFi stateful data from storage.

This includes repository data such as:

- database repository
- FlowFile repository
- content repository
- provenance repository

Helm reinstall alone does not restore queued data or local NiFi repository state.

## Operator Responsibilities

Operators remain responsible for:

- cluster backup strategy
- Secret recovery strategy
- PVC snapshot schedules and restore procedures
- cert-manager issuer and CA recovery
- disaster-recovery drills

## Recovery Guidance

Use the export helper for control-plane recovery planning, and use your storage platform for PVC-backed data recovery.

NiFi-Fabric does not add a separate backup control plane or storage orchestration layer.
