# NiFiDataflow Reference

This page describes the `NiFiDataflow` CRD for NiFi-Fabric.

See also:

- [ADR 0007: Minimal NiFiDataflow CRD](../adr/0007-minimal-nifidataflow-crd.md)
- [Flows](../manage/flows.md)
- [NiFiCluster Reference](nificluster.md)

## Intent

The `NiFiDataflow` resource is a declarative deployment record for one imported versioned flow target.

It is meant to cover:

- initial import of a declared versioned flow
- version change of that imported target
- optional rollback to a prior version
- optional direct attachment of one declared Parameter Context
- explicit ownership and status reporting

It is not meant to cover:

- arbitrary graph editing
- generic runtime object management
- automatic adoption of manual targets
- multi-step promotion workflow orchestration
- full continuous drift correction of live UI edits

## Resource

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiDataflow
metadata:
  name: orders-ingest
spec:
  clusterRef:
    name: nifi
  source:
    registryClient:
      name: github-main
    bucket: platform-flows
    flow: orders-ingest
    version: "12"
  target:
    rootChildProcessGroupName: orders-ingest
    parameterContextRef:
      name: orders-prod
  rollout:
    strategy: DrainAndReplace
    timeout: 20m
  syncPolicy:
    mode: OnChange
  suspend: false
status:
  phase: Ready
  processGroupId: 2f36b6e2-6a61-4b4e-90a8-9dd97b0b1f08
  observedVersion: "12"
  lastSuccessfulVersion: "12"
  conditions:
  - type: Available
    status: "True"
    reason: Reconciled
    message: Imported target is present and matches the declared version.
```

## Spec

### NiFiDataflow

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `apiVersion` | string | Kubernetes API version. | Yes |
| `kind` | string | Resource kind. | Yes |
| `metadata.name` | string | Resource name. | Yes |
| `spec` | object | Desired flow-deployment intent. | Yes |
| `status` | object | Observed controller state. | No |

### NiFiDataflowSpec

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.clusterRef` | object | Managed cluster reference. Uses a name-only same-namespace reference to a managed `NiFiCluster`. | Yes |
| `spec.source` | object | Selected flow source and version. | Yes |
| `spec.target` | object | Owned deployment target for this flow. | Yes |
| `spec.rollout` | object | Conservative typed rollout behavior for version changes. | No |
| `spec.syncPolicy` | object | Reconciliation depth for the owned target. | No |
| `spec.suspend` | boolean | Pauses active reconciliation. | No |

### ClusterRef

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.clusterRef.name` | string | Name of the managed `NiFiCluster` in the same namespace. | Yes |

### Source

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.source.registryClient` | object | Named Flow Registry Client reference. | Yes |
| `spec.source.bucket` | string | Registry bucket name. | Yes |
| `spec.source.flow` | string | Registry flow name. | Yes |
| `spec.source.version` | string | Selected flow version. Supported values are an explicit provider-native identifier or `latest`. | Yes |

### RegistryClientRef

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.source.registryClient.name` | string | Name of the live or declared registry client to use. | Yes |

### Target

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.target.rootChildProcessGroupName` | string | Name of the root-child process group this resource owns. | Yes |
| `spec.target.parameterContextRef` | object | Optional direct Parameter Context attachment. | No |

### ParameterContextRef

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.target.parameterContextRef.name` | string | Name of the declared Parameter Context to attach to the owned target. | Yes |

### Rollout

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.rollout.strategy` | enum | Supported values: `Replace`, `DrainAndReplace`. | No |
| `spec.rollout.timeout` | duration | Maximum time allowed for pre-change drain and post-change settle work. | No |

### SyncPolicy

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `spec.syncPolicy.mode` | enum | Supported values: `Once`, `OnChange`. | No |

## Status

### NiFiDataflowStatus

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `status.phase` | enum | Supported values: `Pending`, `Importing`, `Ready`, `Progressing`, `Blocked`, `Failed`. | No |
| `status.processGroupId` | string | Process group ID for the owned target when resolved. | No |
| `status.observedVersion` | string | Last observed version attached to the owned target. | No |
| `status.lastSuccessfulVersion` | string | Last version the controller observed as successfully reconciled. | No |
| `status.lastOperation` | object | Current or last operation summary. | No |
| `status.conditions[]` | `Condition` list | Machine-readable condition set. | No |

### LastOperation

| Field | Type | Description | Required |
| --- | --- | --- | --- |
| `status.lastOperation.type` | string | Operation type such as `Import`, `Upgrade`, `Rollback`, or `AttachParameterContext`. | No |
| `status.lastOperation.phase` | enum | Supported values: `Pending`, `Running`, `Succeeded`, `Failed`. | No |
| `status.lastOperation.startedAt` | timestamp | When the operation started. | No |
| `status.lastOperation.completedAt` | timestamp | When the operation completed. | No |
| `status.lastOperation.message` | string | Human-readable summary for operators. | No |

### Conditions

Condition types:

- `SourceResolved`
- `TargetResolved`
- `ParameterContextReady`
- `Progressing`
- `Available`
- `Degraded`

## Reconcile Scope

The controller would:

- resolve the declared flow source
- create or find one root-child target by name
- require an explicit ownership marker before mutating an existing target
- attach the declared version
- attach one declared Parameter Context when configured
- report clear blocked or failed states

The controller would not:

- manage arbitrary child placement outside the root level
- mutate arbitrary processors, connections, or controller services
- delete live targets by default on resource deletion
- reconcile unrelated live drift in the surrounding graph

## Failure Handling

The implementation documents and tests at least these cases:

- missing registry client
- missing bucket, flow, or version
- target name collision without ownership marker
- Parameter Context missing or invalid
- rollout drain timeout
- attached version fails to settle cleanly
- delete without removing the live process group by default

## Testing Notes

Current coverage includes:

- import create
- import update to a new declared version
- rollback to a previous version
- `Once` versus `OnChange`
- ownership conflict blocking
- Parameter Context attachment and update
- controller status condition behavior
