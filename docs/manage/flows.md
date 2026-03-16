# Flows

NiFi-Fabric supports bounded versioned-flow import as a runtime-managed config feature.

## What This Feature Does

The app chart can import one declared registry-backed flow per entry into one named root child process group in NiFi and attach that process group to the selected registry-backed version without provider write-back.

Supported bounded content:

- selected live Flow Registry Client name
- selected bucket name
- selected flow name
- selected version, either `latest` or an explicit provider-native version identifier without whitespace
- one intended child process-group name under the root canvas
- optional direct Parameter Context attachment by name

## Product Position

- this feature is optional and disabled by default
- the public API stays under `versionedFlowImports.*`
- the chart does not become a generic flow-runtime, graph-editing, or synchronization manager
- the selected Flow Registry Client must already exist in NiFi in this slice
- runtime reconciliation is intentionally limited to bounded import creation, bounded version-selection resolution, and optional direct Parameter Context attachment

## Configuration Surface

Use app chart values under:

- `versionedFlowImports.enabled`
- `versionedFlowImports.mountPath`
- `versionedFlowImports.imports[]`

Use platform chart values under:

- `nifi.versionedFlowImports.*`

## Ownership and Drift

What the product creates:

- one chart-rendered `ConfigMap` runtime bundle when `versionedFlowImports.enabled=true`
- one pod mount that makes that bundle available inside the NiFi pod
- the declared root-child imported process-group instances in NiFi for product-owned targets

What the product reconciles:

- the rendered Kubernetes runtime bundle
- restart-scoped import of missing declared root-child process groups
- restart-scoped attachment or update of the declared registry, bucket, flow, and selected version for owned imported process groups
- restart-scoped direct Parameter Context attachment for the declared imported process group when one reference is configured

What the product only references:

- the selected live NiFi Flow Registry Client
- the selected registry bucket, flow, and version exposed through that client
- the declared Parameter Context by name when a direct attachment is requested

What remains operator-owned:

- creating and maintaining the live Flow Registry Client instance in NiFi
- registry repository lifecycle and credential lifecycle
- undeclared or manually created process groups
- deleting removed declared imports
- broader graph edits inside or around the imported process group

Manual UI edits outside the bounded import scope are unsupported. The product does not perform ongoing sync, and it does not attempt arbitrary graph reconciliation. If an owned imported process group is changed so that it no longer matches the declared bounded source or placement contract, the runtime bundle reports a blocked status on the next restart-scoped reconcile until the operator restores the expected state or deletes and recreates the owned process group.

## Runtime Contract

- current runtime contract: `Runtime-managed / bounded`
- pod `-0` performs restart-scoped reconciliation after NiFi API readiness
- `auth.mode=singleUser` is required in this slice so the bounded bootstrap can authenticate cleanly
- `authz.capabilities.mutableFlow.enabled=true` with `includeInitialAdmin=true` or `authz.bundles.flowVersionManager.includeInitialAdmin=true` is required so the bootstrap admin path can create the root-child import target
- the selected live Flow Registry Client must already exist in NiFi
- bounded version attachment uses the selected registry-backed snapshot through the NiFi versions API and does not commit a new registry version
- focused runtime proof on the single-node platform path upgrades the release, hydrates the chart-rendered bounded import bundle into pod `-0`, and runs the same bounded bootstrap directly so the existing operator-owned live Flow Registry Client can be reused without claiming single-node pod replacement preserves it
- at most one direct Parameter Context reference is supported per import in this slice
- missing live client, missing selected flow content, or unsupported manual drift is reported as `blocked` in the bootstrap status file instead of widening the feature into a generic recovery loop
- ongoing automatic synchronization to newer registry versions is out of scope

## Support Level

- current support level: `Runtime-managed / focused-proof`
- focused kind proof covers real import of a selected registry-backed flow through the platform chart path
- the resulting root-child process group exists in NiFi with attached version-control state for the selected version, seeded flow content, and bounded Parameter Context attachment verified
- the separate GitHub selection proof still covers provider-native version resolution on the focused workflow path

## Example Overlays

The repo includes:

- [platform-managed-versioned-flow-import-values.yaml](../../examples/platform-managed-versioned-flow-import-values.yaml)
- [platform-managed-versioned-flow-import-kind-values.yaml](../../examples/platform-managed-versioned-flow-import-kind-values.yaml)
- [github-versioned-flow-selection-kind-values.yaml](../../examples/github-versioned-flow-selection-kind-values.yaml)

Template the standard platform example with:

```bash
helm template test charts/nifi-platform \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-versioned-flow-import-values.yaml
```

Focused platform runtime proof:

```bash
make kind-platform-managed-versioned-flow-import-fast-e2e
```

Focused GitHub selection proof:

```bash
make kind-versioned-flow-selection-fast-e2e
```

## What This Feature Does Not Do

- create Flow Registry Clients automatically
- manage arbitrary process-group placement or mutation
- implement generic flow runtime-object management
- perform controller-managed ongoing sync to registry changes
- create a flow CRD or a second product control plane
