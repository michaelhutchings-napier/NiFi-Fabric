# ADR 0007: Minimal NiFiDataflow CRD

- Status: Accepted
- Date: 2026-03-26

## Context

NiFi-Fabric currently supports versioned-flow import through Helm values and an in-pod runtime reconcile loop. That path is intentionally narrow:

- one declared registry-backed import per entry
- one named root-child target per import
- optional direct Parameter Context attachment
- no generic graph editing
- no flow CRD or second broad object-management control plane

That model stays explainable, but it leaves a gap for teams that want a first-class declarative record for flow deployment, upgrade, rollback, and ownership.

The largest product risk is introducing a broad `NiFiDataflow` API that recreates a NiFi object-management operator with hard-to-explain adoption, drift, deletion, and rollout behavior.

## Decision

NiFi-Fabric adds a second CRD as a minimal `NiFiDataflow` resource with a tightly scoped ownership model.

The CRD does:

- represent one imported flow target
- reference one existing managed `NiFiCluster`
- reference one existing flow source through a named Flow Registry Client plus bucket, flow, and version
- target one root-child process group by name
- optionally attach one declared Parameter Context by name
- support conservative rollout semantics for version changes
- report explicit status conditions and last-operation summaries

The CRD does not:

- manage arbitrary NiFi runtime objects
- create a generic process-group graph API
- adopt unowned existing process groups automatically
- continuously force all live UI edits back to a desired state
- create a broad controller-managed promotion engine
- replace Helm as the main configuration surface

## API Shape

The public shape should stay small and boring:

- `spec.clusterRef.name`
- `spec.source.registryClient.name`
- `spec.source.bucket`
- `spec.source.flow`
- `spec.source.version`
- `spec.target.rootChildProcessGroupName`
- `spec.target.parameterContextRef.name`
- `spec.rollout.strategy`
- `spec.rollout.timeout`
- `spec.syncPolicy.mode`
- `spec.suspend`

The initial sync policy should stay narrow:

- `Once`
- `OnChange`

The design intentionally does not include an `Always` mode in the first cut. That mode widens the feature from declarative deployment into general drift reconciliation.

## Ownership Model

`NiFiDataflow` should own only targets it created or targets that carry an explicit matching ownership marker.

If the target name already exists without a matching ownership marker, reconciliation should block and report the conflict instead of adopting the process group.

Deletion behavior should also stay narrow:

- deleting the resource should not remove the process group by default
- destructive teardown should require explicit future design work

## Rollout Model

Version changes should use a small typed rollout policy:

- `Replace`
- `DrainAndReplace`

`Replace` means switching to the declared version without pre-drain sequencing.

`DrainAndReplace` means conservative ordered preparation, version change, settle checks, and clear failure reporting. This path must describe how failures are surfaced when draining times out or the new version does not settle cleanly.

## Status Model

The CRD should report clear controller-runtime style conditions and an explicit last-operation summary.

Condition types:

- `SourceResolved`
- `TargetResolved`
- `ParameterContextReady`
- `Progressing`
- `Available`
- `Degraded`

Status summary fields:

- `status.phase`
- `status.processGroupId`
- `status.observedVersion`
- `status.lastSuccessfulVersion`
- `status.lastOperation`

## Rejected Alternatives

- expanding `versionedFlowImports.*` until it becomes a hidden flow deployment API inside Helm values
- adding multiple runtime-object CRDs at once for flows, users, registry clients, and policies
- supporting arbitrary parent process-group placement in the first cut
- automatic adoption of manually created live process groups
- broad backup or promotion orchestration in the same feature

## Consequences

- NiFi-Fabric could cover a high-value customer workflow without immediately becoming a broad NiFi object-management operator.
- The project moves beyond the original one-CRD MVP, so the feature stays intentionally small and is justified with strong tests and docs.
- The reference docs must explain failure handling for create, update, rollback, ownership conflict, missing source flow, and Parameter Context attachment errors.
- If the feature later needs broader graph management, that should trigger a new ADR rather than incremental expansion by accident.
