# Backup, Restore, and Disaster Recovery

NiFi-Fabric treats backup and recovery as an operator workflow built on a clear
split of responsibility:

- NiFi-Fabric owns declarative install and lifecycle intent
- NiFi owns NiFi runtime behavior
- the storage platform owns PVC snapshot and restore behavior

That split is intentional. The project documents a recovery model, but it does
not become a backup operator, snapshot scheduler, or cross-region DR control
plane.

## The Two-Layer Model

Think about recovery in two layers:

1. control-plane backup
2. data-plane recovery

Control-plane backup preserves the declarative intent needed to rebuild the
platform shape.

Data-plane recovery restores NiFi stateful data and any external dependencies
the runtime still needs after the declarative layer is reinstalled.

## Control-Plane Backup

Control-plane backup means preserving the platform-owned intent, including:

- Helm values and overlays
- rendered manifest intent when bundle workflows are used
- the managed `NiFiCluster` resource intent when present
- references to Secrets, ConfigMaps, issuers, and other prerequisites

NiFi-Fabric includes a thin export helper for this layer:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

The current export bundle includes:

- `helm-values.user.yaml`
- `helm-values.resolved.yaml`
- `helm-manifest.yaml`
- `managed-objects.txt`
- `backup-metadata.json`
- `reference-inventory.json`
- `recovery-checklist.md`
- sanitized live snapshots of referenced objects under `referenced-resources/`
- sanitized `nificluster-intent.json` when a managed `NiFiCluster` exists

This export is intentionally practical rather than magical. It helps operators
reconstruct the declarative control plane, but it does not export cleartext
Secret material or storage-backed NiFi repository data.

For AKS environments, Azure Blob Storage is a reasonable optional destination
for this exported bundle when you want off-cluster storage for the declarative
artifacts. See [AKS](aks.md) for one concrete operator workflow.

### What Control-Plane Export Does Not Capture

The control-plane export does not restore:

- PVC snapshots
- queued FlowFiles
- content, provenance, or repository data
- Secret values from escrow or your secret manager
- cert-manager issuers, CA state, or external PKI systems
- external IdP, registry, ingress, DNS, or storage-platform state

Use the export helper to preserve intent. Use your storage and secret-management
systems to preserve the rest of the environment.

## Data-Plane Recovery

Data-plane recovery means restoring NiFi stateful data from storage and
recreating the external dependencies the deployment still expects.

Helm reinstall alone does not restore queued data or local NiFi repository
state.

PVC-backed repository recovery typically matters for:

- database repository
- FlowFile repository
- content repository
- provenance repository

Additional runtime state may also matter depending on the enabled feature set:

- flow-action archive content under the persisted database repository path when
  flow-action audit is enabled
- any environment-specific extra volumes you layer onto the chart for your own
  backup, archive, or extension workflows

By default, the writable `conf` copy and pod `logs` volume are `emptyDir`
content. Those are not durable recovery sources unless you deliberately replace
or extend them with your own storage pattern. If you enable
`persistence.logs.*`, treat that PVC as operator-owned local retention for
troubleshooting, not as a backup or recovery guarantee.

## Recovery Asset Inventory

Operators should think about recovery assets in four buckets.

### 1. Declarative Platform Intent

Preserve:

- Git-tracked Helm values and overlays
- packaged or rendered manifest inputs when your delivery workflow depends on them
- the exported control-plane bundle above

This is the part NiFi-Fabric helps with directly.

### 2. Operator-Owned References

Verify recovery plans for:

- `Secret/nifi-auth` or any alternate auth-mode Secrets
- `Secret/nifi-tls` or cert-manager-generated TLS output
- PKCS12 password or sensitive-properties Secrets used by the cert-manager path
- referenced ConfigMaps and Secrets surfaced in `reference-inventory.json`
- trust-manager source ConfigMaps or Secrets when that integration is enabled

The export helper inventories these references, but operators still own their
actual recovery.

### 3. External Dependencies

Plan recovery for:

- cert-manager issuers and CA chains
- OIDC or LDAP identity providers
- external Flow Registry systems or Git provider access
- ingress, DNS, and public endpoint dependencies
- storage-class, CSI snapshot, and restore capabilities

NiFi-Fabric can point at these systems, but it does not recover them for you.

### 4. Stateful NiFi Data

Plan storage-level recovery for:

- PVC snapshots and restore procedures
- repository restore ordering and validation
- recovery-point objectives for queued and in-flight data
- platform-specific snapshot consistency and storage guarantees

Keep this layer generic and storage-platform-owned.

## Typed Features and Recovery Planning

Some NiFi-Fabric features help make recovery planning clearer, but they do not
replace storage recovery.

Flow Registry Clients and versioned-flow import help preserve declared flow
source intent:

- the selected registry client, bucket, flow, and declared version stay visible
  in Helm values
- registry-backed flow definitions remain part of the declarative product story
  rather than hidden raw `nifi.registry.*` property snippets
- after control-plane recovery, the same declared import inputs can be rendered
  again through the supported product surface

That is useful for reconstruction of declared flow configuration. It is not the
same thing as recovering:

- queued FlowFiles
- local repository state
- arbitrary manual UI edits outside the managed scope

See [Flow Registry Clients](manage/flow-registry-clients.md) and
[Flows](manage/flows.md) for the supported bounded model.

## Hibernation and Restore Boundary

Managed hibernation and restore are lifecycle features, not backup features.

They are useful when you want to:

- scale the cluster safely to zero
- return it to the prior running size
- preserve the normal controller-owned node-preparation model

They are not a substitute for:

- control-plane export
- secret escrow
- PVC snapshot scheduling
- disaster-recovery drills

See [Hibernation and Restore](manage/hibernation-and-restore.md).

## Operator Checklist

### Before a Failure

- export the control-plane bundle on a regular schedule or before significant changes
- keep Git-tracked values and overlays recoverable outside the cluster
- escrow the Secrets and credentials listed in the reference inventory
- verify cert-manager issuer, CA, and trust-manager dependency recovery
- define PVC snapshot schedules and restore procedures with your storage platform
- know which external IdP and Flow Registry dependencies the cluster still needs
- run recovery drills, not just backups

### After a Failure

1. Restore or recreate operator-owned Secrets, ConfigMaps, issuers, and other prerequisites.
2. Restore PVC-backed NiFi repositories through your storage platform if data-plane recovery is required.
3. Reinstall the declarative control plane from Git or the exported bundle.
4. Use the recovery helper when the export bundle is your practical source of truth:

```bash
bash hack/recover-control-plane-backup.sh \
  --backup-dir ./backup/nifi-control-plane
```

5. Verify the platform layer:

```bash
helm -n nifi status nifi
kubectl -n nifi get nificluster,statefulset,pods,svc,pvc
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

6. Verify runtime-owned bounded features such as versioned-flow import, Parameter Contexts, and metrics integrations only after the base cluster is healthy.

## Recovery Boundaries

NiFi-Fabric supports recovery planning by documenting and exporting intent.

NiFi-Fabric does not guarantee:

- storage-consistent snapshots across all environments
- durable queue recovery without storage-level restore
- cross-region replication or DR topology management
- environment-specific cloud backup automation

If you need those outcomes, layer them on top of the chart and controller using
your storage platform, secret manager, and cloud-native recovery services.
