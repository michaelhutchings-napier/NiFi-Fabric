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

The same bounded runtime path can also consume controller-owned `NiFiDataflow`
declarations through the optional `versionedFlowImports.controllerBridge.*`
mount. That bridge reuses the existing in-pod bounded import reconciler rather
than introducing a separate live flow-management engine. When the controller
bridge is enabled, the same runtime path also mirrors its latest bounded import
status into a controller-observed ConfigMap so `NiFiDataflow.status` can report
live runtime outcomes from the existing pod `-0` reconciler.

## Ownership and Drift

What the product creates:

- one chart-rendered `ConfigMap` runtime bundle when `versionedFlowImports.enabled=true`
- one pod mount that makes that bundle available inside the NiFi pod
- when `versionedFlowImports.controllerBridge.enabled=true`, one chart-rendered runtime status `ConfigMap` that pod `-0` updates with the latest bounded import outcomes
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

Manual UI edits outside the managed import scope are unsupported. The product does not perform ongoing sync, and it does not attempt arbitrary graph reconciliation. Within the product-owned scope, direct version selection and direct Parameter Context attachment may be reconciled back to the declared state. Unsupported drift or same-name operator-owned collisions are reported as `blocked` in the runtime status file until the operator restores the expected state or deletes and recreates the target.

## Behavior

- management model: `Runtime-managed`
- pod `-0` performs live reconciliation after NiFi API readiness
- supported auth modes are `singleUser`, `oidc`, and `ldap`
- `auth.mode=singleUser` requires `authz.capabilities.mutableFlow.enabled=true` with `includeInitialAdmin=true` or `authz.bundles.flowVersionManager.includeInitialAdmin=true` so the import path can create the root-child import target
- `auth.mode=oidc` and `auth.mode=ldap` require `authz.bootstrap.initialAdminIdentity` so the proxied management identity is explicit and operator-visible
- the runtime loop uses the workload TLS certificate as a trusted-proxy client and acts as the declared management identity
- `provider=nifiRegistry` entries can be created and reconciled live by this path
- other supported providers still require a matching live Flow Registry Client to already exist in NiFi
- version attachment uses the selected registry-backed snapshot through the NiFi versions API and does not commit a new registry version
- when NiFi exposes only version metadata and not inline snapshot content, the current fallback supports GitHub and NiFi Registry sources in this feature
- validation on the single-node platform path upgrades the release, lets the live in-pod reconcile loop import the declared flow, and then verifies a later declared version change reconciles without replacing pod `-0`
- at most one direct Parameter Context reference is supported per import in this feature
- `latest` is resolved during create or declared-change reconcile and then pinned through the ownership marker; the product does not keep polling for newer versions once the declaration is unchanged
- missing live client, missing selected flow content, or unsupported manual drift is reported as `blocked` in the runtime status file instead of widening the feature into a generic recovery loop
- when the optional controller bridge is enabled, the same runtime result is surfaced back into `NiFiDataflow.status` from the controller-observed status `ConfigMap`
- when the optional controller bridge is enabled, the controller also emits Kubernetes events for meaningful bounded-runtime transitions such as `Ready`, `Blocked`, and `Failed`
- retained owned imports reported by the bounded runtime are surfaced as warnings on otherwise healthy `NiFiDataflow` resources so operators can see stale retained targets without widening the feature into a deletion controller
- operator-owned targets without the product ownership marker are surfaced as explicit adoption-refused blocked status and events; this path does not auto-adopt them
- when retained owned imports disappear from the bounded runtime status, the controller emits a normalized warning-cleared event so the signal is visible without repeated warning spam
- ongoing automatic synchronization to newer registry versions is out of scope

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

If you want to exercise the NiFi Registry compatibility path locally, run:

```bash
make kind-platform-managed-versioned-flow-import-nifi-registry-fast-e2e
```

If you want to exercise GitHub version selection locally, run:

```bash
make kind-versioned-flow-selection-fast-e2e
```

## What This Feature Does Not Do

- create arbitrary Flow Registry Clients automatically
- manage arbitrary process-group placement or mutation
- implement generic flow runtime-object management
- perform controller-managed ongoing sync to registry changes
- create a flow CRD or a second product control plane
