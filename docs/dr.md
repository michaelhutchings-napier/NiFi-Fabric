# Backup, Restore, and Disaster Recovery

NiFi-Fabric treats backup, restore, and disaster recovery as a first-class production topic, but not as a magic platform-owned feature.

The project is intentionally honest about scope:

- the platform owns a thin declarative control plane
- NiFi owns NiFi-native runtime behavior
- the storage platform owns snapshot and volume recovery behavior
- operators own the recovery plan that ties those layers together

## The Two-Layer Model

NiFi-Fabric separates DR into two layers:

1. control-plane backup
2. data-plane recovery posture

This prevents the platform from over-claiming ownership of NiFi internals while still giving teams a production-grade model.

## Layer A: Control-Plane Backup

Control-plane backup is the recovery of declarative intent.

This layer should be backed up as the source of truth for:

- `charts/nifi-platform` values and overlays
- `charts/nifi` values and overlays
- rendered release intent when manifest bundles are used
- the `NiFiCluster` resource in managed mode
- chart-managed config for:
  - TLS mode and references
  - auth mode and references
  - metrics mode and references
  - optional trust-manager integration
  - typed Site-to-Site metrics, status, and provenance sender configuration
  - Flow Registry Client catalog content
  - autoscaling policy
  - hibernation settings
  - ingress or Route settings
  - bounded authz bootstrap bundles

This layer should also include the operator source of truth for:

- referenced Secrets and ConfigMaps
- cert-manager issuer references and issuer configuration
- trust-manager source objects and bundle intent
- any GitOps metadata needed to reapply the release

What control-plane backup restores well:

- a fresh cluster or namespace with the same platform intent
- the same chart-managed wiring and `NiFiCluster` lifecycle policy
- the same TLS, auth, metrics, trust, and bounded feature configuration

What it does not restore by itself:

- queued data
- repository contents
- local NiFi runtime state stored on PVCs
- cert-manager private keys or external secret-manager contents unless those systems are also backed up

### MVP Export Workflow

The control-plane MVP now includes a thin export helper:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

The bundle is intentionally human-readable and audit-friendly. It exports:

- `helm-values.user.yaml`
- `helm-values.resolved.yaml`
- `helm-manifest.yaml`
- `managed-objects.txt`
- `nificluster-intent.json` when a managed `NiFiCluster` exists
- `reference-inventory.json`
- `referenced-resources/` with metadata-only Secret snapshots and sanitized ConfigMap or Secret captures where available
- `recovery-checklist.md`
- `backup-metadata.json`

How to treat those artifacts:

- `helm-values.user.yaml` is the closest snapshot to the original release intent and should map cleanly back to GitOps overlays
- `helm-values.resolved.yaml` is the practical recovery fallback when the original overlay composition is missing
- `helm-manifest.yaml` and `managed-objects.txt` are the audit trail for the chart-managed control-plane objects that were present at export time
- `nificluster-intent.json` is a sanitized intent snapshot, not a replacement for the standard Helm-centered managed recovery path
- `reference-inventory.json` and `referenced-resources/` are the recovery checklist for operator-owned dependencies

## Layer B: Data-Plane Recovery Posture

Data-plane recovery is the recovery of NiFi stateful data.

NiFi-Fabric uses persistent volumes for NiFi repositories. That means production data recovery depends on the storage layer, not just on Helm reapply.

The key repository areas are:

- database repository
- FlowFile repository
- content repository
- provenance repository

What redeploy plus config can recover:

- Kubernetes objects and workload wiring
- NiFi pod topology and cluster shape
- chart-managed bootstrap configuration
- typed sender-side Site-to-Site runtime objects recreated from bounded config

What requires PVC restore or snapshots:

- queued FlowFiles
- content claims
- provenance events
- other local runtime state persisted on the repository volumes

## PVC Snapshot Guidance

NiFi-Fabric does not create or manage snapshots.

Recommended operator posture:

- use a storage class with clear snapshot and restore behavior
- snapshot the full repository set as one recovery unit
- understand whether the platform provides crash-consistent only or stronger guarantees
- test restore timing and attachment behavior on the target cluster
- document whether snapshots are zonal, regional, or cross-cluster restorable

Important caution:

- restoring only some repository PVCs while leaving others at a different point in time is risky and should be treated as an operator-led exception case, not a normal product workflow

## Realistic RPO and RTO Guidance

NiFi-Fabric intentionally does not publish fake universal RPO or RTO numbers.

Instead:

- control-plane RPO can be near zero when release intent lives in Git
- data-plane RPO depends on snapshot cadence or upstream replayability
- control-plane RTO is usually the time to recreate prerequisites and reapply Helm
- data-plane RTO is dominated by snapshot restore speed, PVC reattachment, NiFi repository recovery, and cluster-size-specific settling time

Practical interpretation:

- redeploy-only restore is a config restore
- snapshot-backed restore is a stateful NiFi recovery exercise
- teams that need low data-loss objectives must design snapshot cadence and upstream replay around that need

## Product-Supported Scope

NiFi-Fabric supports:

- stable declarative recovery through `charts/nifi-platform`, `charts/nifi`, and `NiFiCluster`
- chart-managed references for TLS, auth, metrics, trust-manager, and Flow Registry Clients
- controller-owned lifecycle behavior for rollout, TLS restart handling, hibernation, restore, and built-in autoscaling
- bounded typed Site-to-Site sender features recreated from config
- thin export and reinstall helpers for control-plane recovery

NiFi-Fabric does not support:

- generic NiFi runtime-object backup or restore
- provider write-back
- storage snapshot orchestration
- automatic recovery of all NiFi internal state from the control plane alone
- a new backup CRD or a generic DR control plane

## Operator-Owned Scope

Operators remain responsible for:

- Kubernetes cluster backup strategy
- namespace and Secret recovery strategy
- PVC snapshot schedules, retention, and restore procedures
- cert-manager issuer and CA recovery
- trust-manager source object recovery
- external identity, Secret manager, and certificate authority dependencies
- disaster-recovery drills and acceptance testing
- deciding when redeploy-only recovery is acceptable versus when snapshot restore is required

## Interaction with Existing Features

### charts/nifi-platform, charts/nifi, and NiFiCluster

These are part of the control-plane backup scope and should be recoverable from declarative source control.

`NiFiCluster` remains the lifecycle API for managed mode. DR documentation does not expand it into a generic backup-management API.

The MVP recovery path stays centered on:

- reinstalling `charts/nifi-platform` or `charts/nifi` with the exported Helm values snapshot
- using the exported `NiFiCluster` intent as a reference artifact, not as a second primary control plane

### cert-manager

cert-manager remains the certificate lifecycle engine when enabled.

DR implication:

- the platform can restore the `Certificate` intent
- operators still need issuer, CA, and private-key recovery planning
- if a DR environment issues new certificates, trust and identity assumptions must be reviewed rather than assumed

### trust-manager

trust-manager remains an optional CA-distribution layer.

DR implication:

- the platform can restore bundle intent and references
- operators still need the trust-manager installation, source objects, and any secret-target permissions restored
- trust-manager is not a replacement for storage or certificate recovery

### Authentication

The platform can restore auth mode and references.

Operators still own:

- external IdP availability
- LDAP or OIDC dependency recovery
- referenced Secret material
- any user or group state that is external to the chart-managed bootstrap model

### Metrics and Typed Site-to-Site Features

The platform can restore metrics wiring and typed Site-to-Site sender configuration.

Operators still own:

- metrics backend continuity
- receiver-side Site-to-Site topology and policies
- long-lived credential lifecycle
- downstream data retention and replay expectations

### Autoscaling and Optional KEDA

Autoscaling policy is part of the control plane.

DR implication:

- restoring `NiFiCluster` and values restores the intended autoscaling posture
- it does not restore in-flight queue state or historical pressure evidence from lost PVCs
- KEDA remains optional external intent, not a second recovery control plane

### Hibernation and Restore

Hibernation and restore are lifecycle features, not a substitute for DR.

They help with:

- orderly scale-to-zero
- restore to the previous running size

They do not by themselves provide:

- cross-cluster disaster recovery
- repository backup
- queue preservation without durable PVCs

### Flow Registry Clients

Prepared Flow Registry Client catalog content is part of the control-plane backup scope.

That can help reconstruct client configuration, but it does not turn the platform into a full flow-content recovery system.

Future flow or parameter-context features should follow the same rule:

- declarative intent may become part of control-plane backup scope
- generic NiFi runtime-object or provider write-back management remains out of scope unless explicitly designed and proven later

## Recommended Production Checklist

- keep Helm values, overlays, and `NiFiCluster` definitions in Git
- define a recovery source of truth for referenced Secrets and issuers
- use durable storage classes for all repository PVCs
- define snapshot cadence and retention for the full repository set
- document whether the target storage can restore snapshots into a new cluster
- test redeploy-only recovery and snapshot-backed recovery separately
- document realistic application-level RPO and RTO expectations for the environment

## Recovery Workflow

The control-plane MVP recovery helper is:

```bash
bash hack/recover-control-plane-backup.sh \
  --backup-dir ./backup/nifi-control-plane
```

By default it:

- reads the exported bundle metadata
- chooses the chart hint captured at export time
- uses `helm-values.resolved.yaml`
- runs `helm upgrade --install` to reconstruct the release

You can override the chart, release, namespace, or choose the user-values snapshot instead:

```bash
bash hack/recover-control-plane-backup.sh \
  --backup-dir ./backup/nifi-control-plane \
  --chart charts/nifi-platform \
  --release nifi \
  --namespace nifi \
  --values-mode user
```

Recovery order:

1. Restore or recreate referenced Secrets, ConfigMaps, cert-manager issuers, trust-manager sources, and similar prerequisites listed in `reference-inventory.json`.
2. Reinstall the chart from the repo root with the recovery helper or the equivalent Helm command.
3. Verify the managed control-plane objects and `NiFiCluster` lifecycle surface after Helm reconciliation.
4. Only then evaluate whether data-plane PVC recovery is also required.

## Bounded Restore Workflow Proof

The repository now includes a focused restore workflow proof that stays within the bounded supported feature set:

1. install `charts/nifi-platform`
2. create the runtime Flow Registry Client from the restored catalog
3. let the runtime-managed Parameter Context bootstrap reconcile the declared context from restored config
4. resolve the selected versioned flow from the restored bounded import catalog
5. import the registry-backed flow snapshot and attach the reconciled Parameter Context

What this proof restores:

- the product-facing platform release
- the managed `NiFiCluster` lifecycle surface
- chart-managed Flow Registry Client and bounded flow-selection catalogs plus runtime-managed Parameter Context config
- operator-driven runtime recreation of a functional flow configuration from those restored catalogs and registry-backed flow content

What this proof does not restore:

- queued FlowFiles
- content or provenance repositories
- previously imported flow state from PVCs
- generic NiFi internal runtime state

The proof is intentionally honest about the boundary:

- the platform reinstall and bounded catalogs are product-owned
- runtime Flow Registry Client creation remains operator-driven in this slice
- final flow import and direct Parameter Context attachment are product-owned only within the declared bounded `versionedFlowImports.*` scope after the restored release starts
- declared Parameter Context creation or update is product-owned once the restored release starts
- deleting the NiFi PVCs during the proof is deliberate, so the gate proves config-and-flow reconstruction rather than PVC persistence

Current focused limit:

- the import phase currently proves the `version=latest` bounded restore path on the GitHub-compatible evaluator
- broader historical version replay and broader provider parity remain future work

## Support Boundary

Supported and honest:

- declarative platform recovery
- documented PVC-backed data-plane recovery posture
- explicit separation of platform recovery from storage recovery

Not claimed:

- automatic full NiFi internal recovery from product config alone
- storage-vendor-specific backup automation
- generic provider write-back or generic runtime-object management
