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
- runtime reconciliation is intentionally limited to bounded import creation, bounded version-selection resolution, optional direct Parameter Context attachment, and explicit ownership-marker maintenance for the imported root-child process group

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
- an ownership marker in the imported process-group comments for each declared product-owned target

What the product reconciles:

- the rendered Kubernetes runtime bundle
- live import of missing declared root-child process groups on pod `-0`
- live attachment or update of the declared registry, bucket, flow, and selected version for owned imported process groups
- live direct Parameter Context attachment or detachment for the declared imported process group when one reference is configured
- the ownership marker metadata used to keep the bounded owned scope explicit and auditable

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
- changing an owned import to point at a different declared source flow or different target name; create a new owned target or delete the old one instead

Manual UI edits outside the bounded import scope are unsupported. The product does not perform ongoing sync, and it does not attempt arbitrary graph reconciliation. Within the bounded owned scope, direct version selection and direct Parameter Context attachment may be reconciled back to the declared state. Unsupported drift or same-name operator-owned collisions are reported as `blocked` in the runtime status file until the operator restores the expected state or deletes and recreates the target.

## Runtime Contract

- current runtime contract: `Runtime-managed / bounded`
- pod `-0` performs live reconciliation after NiFi API readiness
- supported auth modes are `singleUser`, `oidc`, and `ldap`
- `auth.mode=singleUser` requires `authz.capabilities.mutableFlow.enabled=true` with `includeInitialAdmin=true` or `authz.bundles.flowVersionManager.includeInitialAdmin=true` so the bounded import path can create the root-child import target
- `auth.mode=oidc` and `auth.mode=ldap` require `authz.bootstrap.initialAdminIdentity` so the proxied management identity is explicit and operator-visible
- the runtime loop uses the workload TLS certificate as a trusted-proxy client and acts as the declared management identity
- the selected live Flow Registry Client must already exist in NiFi
- bounded version attachment uses the selected registry-backed snapshot through the NiFi versions API and does not commit a new registry version
- when NiFi exposes only version metadata and not inline snapshot content, the current bounded fallback still depends on the prepared GitHub client path in this slice
- focused runtime proof on the single-node platform path upgrades the release, lets the live in-pod reconcile loop import the declared flow, and then proves a later declared version change reconciles without replacing pod `-0`
- at most one direct Parameter Context reference is supported per import in this slice
- `latest` is resolved during create or declared-change reconcile and then pinned through the ownership marker; the product does not keep polling for newer versions once the declaration is unchanged
- missing live client, missing selected flow content, or unsupported manual drift is reported as `blocked` in the runtime status file instead of widening the feature into a generic recovery loop
- ongoing automatic synchronization to newer registry versions is out of scope

## Support Level

- current support level: `Runtime-managed / focused-proof`
- focused kind proof covers real import of a selected registry-backed flow through the platform chart path
- the resulting root-child process group exists in NiFi with attached version-control state for the selected version, seeded flow content, bounded Parameter Context attachment, and explicit ownership marker verified
- the same focused proof then changes the declared version and proves the owned import updates live without replacing pod `-0`
- the separate GitHub selection proof still covers provider-native version resolution on the focused workflow path
- enterprise auth support is rendered and runtime-coded, but the focused runtime proof in this repository remains on the standard `singleUser` path today

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
