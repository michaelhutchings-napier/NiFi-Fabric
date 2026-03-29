# AKS

AKS is the primary supported target environment for NiFi-Fabric.

The recommended AKS deployment model is the standard managed install through `charts/nifi-platform` with cert-manager-first TLS.

## Recommended AKS Shape

Use this shape on AKS:

- `charts/nifi-platform`
- managed mode
- cert-manager-first TLS
- PVC-backed NiFi repositories
- internal `ClusterIP` service first, then add ingress or a load balancer only when your deployment needs it

This is the main product path for AKS support and documentation.

## What You Need On AKS

Prepare:

- an AKS cluster with a suitable PVC-capable `StorageClass`
- a controller image reachable by the cluster
- a NiFi image reachable by the cluster
- cert-manager and the referenced `Issuer` or `ClusterIssuer`

If you use Azure Container Registry for the controller image, make sure the AKS kubelet identity can pull from that registry before the first Helm install.

## Install Starting Point

Start with:

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md) when you need explicit inputs
- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Operations and Troubleshooting](operations.md)

## Azure Blob for Control-Plane Backup Artifacts

On AKS, Azure Blob Storage is a reasonable operator-managed destination for the
exported control-plane backup bundle described in [Backup, Restore, and
Disaster Recovery](dr.md).

This remains an operator workflow, not a built-in NiFi-Fabric backup feature.
Use it when you want a durable off-cluster location for the exported
declarative bundle. Do not treat it as a substitute for Secret escrow, PVC
snapshots, or storage-level recovery procedures.

Before using this pattern, make sure you have:

- a Storage Account and Blob container dedicated to backup artifacts
- Azure CLI access through `az login`, workload identity, or equivalent
- a naming and retention convention for backup prefixes
- separate Secret and PVC recovery procedures for the same environment

Example workflow:

```bash
BACKUP_DIR="./backup/nifi-control-plane-$(date -u +%Y%m%dT%H%M%SZ)"

bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir "${BACKUP_DIR}"

az storage blob upload-batch \
  --auth-mode login \
  --destination nifi-fabric-backups \
  --destination-path "prod/nifi/$(basename "${BACKUP_DIR}")" \
  --source "${BACKUP_DIR}"
```

That workflow stores the exported control-plane bundle in Azure Blob while
keeping the export helper and recovery helper unchanged. Teams can run the same
sequence from CI/CD or a scheduled operator job instead of a local shell.

When you need the bundle back for recovery planning or a namespace rebuild,
download the same prefix and point the recovery helper at the restored
directory:

```bash
mkdir -p ./restore

az storage blob download-batch \
  --auth-mode login \
  --source nifi-fabric-backups \
  --pattern "prod/nifi/<backup-timestamp>/*" \
  --destination ./restore

bash hack/recover-control-plane-backup.sh \
  --backup-dir "./restore/prod/nifi/<backup-timestamp>"
```

Keep the boundaries clear:

- Azure Blob in this workflow stores only exported control-plane artifacts
- it does not capture PVC-backed NiFi repositories or queued FlowFiles
- it does not recover external issuers, IdP state, DNS, or ingress
- it does not replace storage snapshots or platform DR procedures

If you need full recovery, pair this Blob workflow with the normal operator
assets described in [Backup, Restore, and Disaster Recovery](dr.md).

## Support Position

NiFi-Fabric works on AKS, and AKS is the primary target environment for the project.

The recommended customer path on AKS is the standard managed install with cert-manager. Advanced shapes remain available, but support and documentation center on that managed AKS path first.
