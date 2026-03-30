# Flows

NiFi-Fabric supports versioned-flow import as a managed configuration feature.

## What This Feature Does

The app chart can import one declared registry-backed flow per entry into one named root child process group in NiFi and attach that process group to the selected registry-backed version without provider write-back.

Supported content:

- selected Flow Registry Client name
- selected bucket name
- selected flow name
- selected version, either `latest` or an explicit provider-native version identifier without whitespace
- one intended child process-group name under the root canvas
- optional direct Parameter Context attachment by name

## Product Position

- this feature is optional and disabled by default
- the public API stays under `versionedFlowImports.*`
- the chart does not become a generic flow-runtime, graph-editing, or synchronization manager
- Git-based Flow Registry Clients remain the preferred long-term direction
- NiFi Registry support in this path is available for NiFi `2.x` environments
- live reconciliation is intentionally limited to import creation, version-selection resolution, optional direct Parameter Context attachment, and explicit ownership-marker maintenance for the imported root-child process group

## Configuration Surface

Use app chart values under:

- `versionedFlowImports.enabled`
- `versionedFlowImports.mountPath`
- `versionedFlowImports.controllerBridge.*`
- `versionedFlowImports.imports[]`

Use platform chart values under:

- `nifi.versionedFlowImports.*`

The same runtime path can also consume `NiFiDataflow` declarations
through the optional `versionedFlowImports.controllerBridge.*` mount. The chart
renders the bridge `ConfigMap` up front so the pod mount is always present, and
the controller only updates the `imports.json` payload inside that Helm-owned
object. That bridge reuses the existing in-pod import reconciler rather
than introducing a separate live flow-management engine. When the controller
bridge is enabled, the same runtime path also mirrors its latest import
status into a controller-observed ConfigMap so `NiFiDataflow.status` can report
live runtime outcomes from the existing pod `-0` reconciler.

## Ownership and Drift

What the product creates:

- one chart-rendered `ConfigMap` runtime bundle when `versionedFlowImports.enabled=true`
- one pod mount that makes that bundle available inside the NiFi pod
- when `versionedFlowImports.controllerBridge.enabled=true`, one chart-rendered runtime status `ConfigMap` that pod `-0` updates with the latest import outcomes
- for declared `provider=nifiRegistry` imports only, one live product-owned Flow Registry Client in NiFi when it does not already exist
- the declared root-child imported process-group instances in NiFi for product-owned targets
- an ownership marker in the imported process-group comments for each declared product-owned target

What the product reconciles:

- the rendered Kubernetes runtime bundle
- for product-owned declared `provider=nifiRegistry` imports only, the live Flow Registry Client name, URL, optional SSL context reference, and explicit ownership marker
- live import of missing declared root-child process groups on pod `-0`
- live attachment or update of the declared registry, bucket, flow, and selected version for owned imported process groups
- live direct Parameter Context attachment or detachment for the declared imported process group when one reference is configured
- the ownership marker metadata used to keep the owned scope explicit and auditable
- the controller-side projection of the runtime status `ConfigMap` back into `NiFiDataflow.status`

What the product references:

- the selected live NiFi Flow Registry Client for providers other than `provider=nifiRegistry`
- the selected registry bucket, flow, and version exposed through that client
- the declared Parameter Context by name when a direct attachment is requested

What remains operator-owned:

- creating and maintaining live Flow Registry Client instances for providers other than `provider=nifiRegistry`
- undeclared or operator-owned live Flow Registry Clients, including same-name collisions the product did not mark as owned
- registry storage lifecycle and credential lifecycle
- undeclared or manually created process groups
- deleting removed declared imports
- broader graph edits inside or around the imported process group
- changing an owned import to point at a different declared source flow or different target name; create a new owned target or delete the old one instead

Manual UI edits outside the managed import scope are unsupported. The product does not perform ongoing sync, and it does not attempt arbitrary graph reconciliation. Within the product-owned scope, direct version selection and direct Parameter Context attachment may be reconciled back to the declared state. When the optional `NiFiDataflow` bridge declares `spec.syncPolicy.mode=Once`, version drift on an already successful owned target is observed but not healed again until the declaration changes. Unsupported drift or same-name operator-owned collisions are reported as `blocked` in the runtime status file until the operator restores the expected state or deletes and recreates the target.

From a recovery-planning perspective, this feature helps preserve declared flow
source intent in Helm values and typed product surfaces. It does not replace
storage-backed recovery for queued data or arbitrary runtime state outside the
bounded managed scope.

For the optional `NiFiDataflow` bridge path, version changes also honor a small
typed rollout policy:

- `Replace` updates the owned imported process group to the resolved version
  without first stopping descendant components
- `DrainAndReplace` first asks NiFi to stop descendant components in the owned
  target, waits for the process group to go quiescent within the declared
  timeout, performs the version change, and resumes descendants only when the
  target was running before the drain step

The rollout contract stays intentionally small. The product does not become a
generic queue-drain or graph-lifecycle engine. If the drain does not
settle before the declared timeout, or if the version change fails after the
target is quiesced, the runtime reports a clear failure and leaves operator
follow-up explicit.

## Behavior

- management model: `Runtime-managed`
- pod `-0` performs live reconciliation after NiFi API readiness
- supported auth modes are `singleUser`, `oidc`, and `ldap`
- `auth.mode=singleUser` requires `authz.capabilities.mutableFlow.enabled=true` with `includeInitialAdmin=true` or `authz.bundles.flowVersionManager.includeInitialAdmin=true` so the import path can create the root-child import target
- `auth.mode=oidc` and `auth.mode=ldap` require `authz.bootstrap.initialAdminIdentity` so the proxied management identity is explicit and operator-visible
- the runtime loop uses the workload TLS certificate as a trusted-proxy client and acts as the declared management identity
- `provider=nifiRegistry` entries can be created and reconciled live by this path
- other supported providers still require a matching live Flow Registry Client to already exist in NiFi
- Azure DevOps-backed client catalogs are supported, and this feature uses the
  same generic pre-created-live-client requirement that applies to other
  non-`nifiRegistry` providers
- version attachment uses the selected registry-backed snapshot through the NiFi versions API and does not commit a new registry version
- when NiFi exposes only version metadata and not inline snapshot content, the current fallback supports GitHub and NiFi Registry sources in this feature
- validation on the single-node platform path upgrades the release, lets the live in-pod reconcile loop import the declared flow, and then verifies a later declared version change reconciles without replacing pod `-0`
- at most one direct Parameter Context reference is supported per import in this feature
- `latest` is resolved during create or declared-change reconcile and then pinned through the ownership marker; the product does not keep polling for newer versions once the declaration is unchanged
- when the optional controller bridge is enabled and a `NiFiDataflow` declares `spec.syncPolicy.mode=Once`, the runtime stops reconciling version drift after the first successful import of that same declaration and only resumes version reconciliation when the spec changes
- when the optional controller bridge is enabled and a `NiFiDataflow` declares `spec.rollout.strategy=DrainAndReplace`, version updates stop descendant components through the NiFi Flow API, wait for `runningCount=0` and `activeThreadCount=0`, then apply the new version and resume only if the target had running descendants before the drain step
- when the optional controller bridge is enabled and a `NiFiDataflow` declares `spec.rollout.strategy=Replace`, version updates switch directly to the resolved version without pre-stop sequencing
- missing live client, missing selected flow content, or unsupported manual drift is reported as `blocked` in the runtime status file instead of widening the feature into a generic recovery loop
- when the optional controller bridge is enabled, the same runtime result is surfaced back into `NiFiDataflow.status` from the controller-observed status `ConfigMap`
- when the optional controller bridge is enabled, the controller also emits Kubernetes events for meaningful runtime transitions such as `Ready`, `Blocked`, and `Failed`
- retained owned imports reported by the runtime are surfaced as warnings on otherwise healthy `NiFiDataflow` resources so operators can see stale retained targets without widening the feature into a deletion controller
- operator-owned targets without the product ownership marker are surfaced as explicit adoption-refused blocked status and events; this path does not auto-adopt them
- when retained owned imports disappear from runtime status, the controller emits a normalized warning-cleared event so the signal is visible without repeated warning spam
- the same bridge path also projects a small queryable status summary under `status.ownership` and `status.warnings.retainedOwnedImports[]` so automation can inspect ownership and retained-warning state without parsing condition text
- `kubectl get nifidataflows` also surfaces a compact operator summary through the `Ownership` and low-priority `Retained` printer columns
- ongoing automatic synchronization to newer registry versions is out of scope

## Operator View

When the optional controller bridge is enabled, operators can inspect the
runtime summary directly from `kubectl` without opening the full
resource:

```bash
kubectl get nifidataflows
```

Example output:

```text
NAME             PHASE    CLUSTER   VERSION   OWNERSHIP        AGE
orders-ingest    Ready    nifi      12        Managed          18m
payments-sync    Blocked  nifi      8         AdoptionRefused  6m
```

If you want the retained-warning hint as well, ask for wide output:

```bash
kubectl get nifidataflows -o wide
```

Example wide output:

```text
NAME             PHASE    CLUSTER   VERSION   OWNERSHIP        RETAINED         AGE
orders-ingest    Ready    nifi      12        Managed          old-orders       18m
payments-sync    Blocked  nifi      8         AdoptionRefused                   6m
```

Column meaning:

- `Ownership` shows the current ownership view from
  `status.ownership.state`, such as `Managed` or `AdoptionRefused`
- `Retained` shows the first retained owned-import warning name when the runtime
  reports stale retained targets under `status.warnings.retainedOwnedImports[]`

For the broader operator recovery model, see
[Backup, Restore, and Disaster Recovery](../dr.md).

## Status Examples

Managed target with no ownership conflict:

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiDataflow
metadata:
  name: orders-ingest
spec:
  clusterRef:
    name: nifi
status:
  phase: Ready
  observedVersion: "12"
  ownership:
    state: Managed
    reason: OwnedTargetReconciled
    message: runtime reconciled owned target orders-ingest at version 12
  warnings: {}
```

Operator-owned target with the same name but without the product ownership
marker:

```yaml
apiVersion: platform.nifi.io/v1alpha1
kind: NiFiDataflow
metadata:
  name: payments-sync
spec:
  clusterRef:
    name: nifi
status:
  phase: Blocked
  observedVersion: "8"
  ownership:
    state: AdoptionRefused
    reason: AdoptionRefused
    message: runtime refused to adopt existing target payments-sync without an ownership marker
  conditions:
  - type: Degraded
    status: "True"
    reason: AdoptionRefused
```

Those examples line up with the `kubectl get nifidataflows` output above:

- `Managed` becomes the `Ownership` column value for a product-owned target the runtime can keep reconciling
- `AdoptionRefused` becomes the `Ownership` column value when a same-name target exists but NiFi-Fabric refuses to auto-adopt it

## Runtime Coverage

- status: `Runtime-managed`
- local kind coverage includes real import of a selected registry-backed flow through the platform chart path
- the resulting root-child process group exists in NiFi with attached version-control state for the selected version, seeded flow content, direct Parameter Context attachment, and explicit ownership marker verified
- the same verification flow then changes the declared version and verifies the owned import updates live without replacing pod `-0`
- the separate GitHub selection flow covers provider-native version resolution on the documented workflow path
- the NiFi Registry compatibility flow covers typed live client creation, bucket and flow resolution, explicit historical version import, and later reconcile back to `latest` through a real in-cluster `apache/nifi-registry` service
- enterprise auth support is rendered and implemented, but repository runtime verification for this feature remains on the standard `singleUser` path today

## Example Overlays

The project includes:

- [platform-managed-versioned-flow-import-values.yaml](../../examples/platform-managed-versioned-flow-import-values.yaml)
- [platform-managed-versioned-flow-import-kind-values.yaml](../../examples/platform-managed-versioned-flow-import-kind-values.yaml)
- [platform-managed-nifidataflow-values.yaml](../../examples/platform-managed-nifidataflow-values.yaml)
- [platform-managed-versioned-flow-import-nifi-registry-values.yaml](../../examples/platform-managed-versioned-flow-import-nifi-registry-values.yaml)
- [platform-managed-versioned-flow-import-nifi-registry-kind-values.yaml](../../examples/platform-managed-versioned-flow-import-nifi-registry-kind-values.yaml)
- [github-versioned-flow-selection-kind-values.yaml](../../examples/github-versioned-flow-selection-kind-values.yaml)

Template the standard platform example with:

```bash
helm template test charts/nifi-platform \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-versioned-flow-import-values.yaml
```

If you want to exercise the platform path locally, run:

```bash
make kind-platform-managed-versioned-flow-import-fast-e2e
```

If you want to exercise the `NiFiDataflow` CRD path locally, run:

```bash
make kind-nifidataflow-fast-e2e
```

If you want to exercise the NiFi Registry compatibility path locally, run:

```bash
make kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e
```

If you want to exercise GitHub version selection locally, run:

```bash
make kind-versioned-flow-selection-fast-e2e
```

For the planned operator workflow for declared version changes, see
[Operations playbooks](../operations/playbooks.md).

## What This Feature Does Not Do

- create arbitrary Flow Registry Clients automatically
- manage arbitrary process-group placement or mutation
- implement generic flow runtime-object management
- perform controller-managed ongoing sync to registry changes
- create a flow CRD or a second product control plane
