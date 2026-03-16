# Parameter Contexts

NiFi-Fabric supports Parameter Contexts as a bounded runtime-managed production feature.

## What This Feature Does

The app chart can create, update, delete, and optionally attach declared Parameter Contexts in NiFi without turning the product into a generic flow-runtime manager.

Supported bounded content:

- named Parameter Context definitions
- non-sensitive inline parameter values
- sensitive parameter references backed by Kubernetes Secrets
- small external Parameter Provider references that document an operator-managed NiFi provider name and optional parameter group
- optional direct root-child process-group attachments declared by name

## Product Position

- this feature is optional and disabled by default
- the public API stays under `parameterContexts.*`
- the chart does not become a generic flow-runtime or graph-editing manager
- Parameter Provider support in this slice is reference-only; the product does not create or reconcile providers in NiFi
- runtime reconciliation is intentionally limited to create, update, delete, and bounded root-child attachment of declared Parameter Contexts only

## Configuration Surface

Use app chart values under:

- `parameterContexts.enabled`
- `parameterContexts.mountPath`
- `parameterContexts.contexts[]`

Use platform chart values under:

- `nifi.parameterContexts.*`

## Ownership and Drift

What the product creates:

- one chart-rendered `ConfigMap` bundle when `parameterContexts.enabled=true`
- one pod mount that makes that bundle available inside the NiFi pod
- the declared Parameter Contexts in NiFi for product-owned names
- bounded direct root-child process-group assignments declared under those owned contexts

What the product reconciles:

- the rendered Kubernetes `ConfigMap` content
- declared Parameter Context values in NiFi on pod `-0`
- deletion of removed product-owned contexts
- direct root-child process-group attachments declared under `attachments[]`

What the product only references:

- Kubernetes Secrets referenced by sensitive parameters
- external NiFi Parameter Providers named in `providerRefs[]`

What remains operator-owned:

- creating or refreshing NiFi Parameter Providers
- deciding how external provider values are fetched into NiFi
- undeclared or manually created Parameter Contexts
- any broader process-group assignment beyond the declared direct root-child attachment scope
- deciding whether operator-owned contexts should ever be deleted

Manual NiFi UI edits to product-owned contexts are reconciled back to the declared bounded state by the live runtime loop. Removed product-owned contexts are deleted. Undeclared contexts remain operator-owned and are not adopted automatically, even if their names collide with declared names.

## Sensitive Values Contract

- non-sensitive parameters use inline `value`
- sensitive parameters must use `secretRef.name` plus `secretRef.key`
- the chart validates that a parameter uses exactly one source
- the chart does not copy Secret values into the rendered catalog
- sensitive values are applied to NiFi from projected Secret files sourced from the referenced Kubernetes Secret
- Secret changes are picked up by the live reconcile loop without requiring a pod restart

## External Provider References

`providerRefs[]` is intentionally small:

- `name` documents the existing NiFi Parameter Provider to use
- `parameterGroup` can document the expected provider-side group or collection when that is meaningful
- these references are advisory and status-visible only; they do not create controller services, providers, or generic provider APIs

## Runtime Contract

- current runtime contract: `Runtime-managed / bounded`
- pod `-0` performs live create, update, delete, and bounded attachment reconciliation for declared contexts after NiFi API readiness
- supported auth modes are `singleUser`, `oidc`, and `ldap`
- `auth.mode=oidc` and `auth.mode=ldap` require `authz.bootstrap.initialAdminIdentity` so the proxied management identity is explicit and operator-visible
- the runtime loop uses the workload TLS certificate as a trusted-proxy client and acts as the declared management identity
- provider references stay reference-only
- process-group attachment is limited to direct root-child process groups declared by name under `attachments[]`
- missing Secret material fails fast in the bootstrap status file and pod logs
- live updates depend on the normal Kubernetes projected `ConfigMap` and Secret refresh window inside the running pod; they are live reconcile events, not forced pod restarts
- attachment target errors fail clearly in the bootstrap status file and pod logs
- same-name operator-owned contexts are not adopted automatically; reconcile fails clearly until the name collision is removed or renamed

## Support Level

- current support level: `Runtime-managed / focused-proof`
- focused kind proof covers declared context creation, live update without pod replacement, deletion of removed owned contexts, inline and Secret-backed values, and bounded direct root-child attachment
- Parameter Provider creation remains out of scope, so the feature stays narrow and explainable

## Example Overlay

The repo includes one platform-chart example:

- [platform-managed-parameter-contexts-values.yaml](../../examples/platform-managed-parameter-contexts-values.yaml)
- [platform-managed-parameter-contexts-kind-values.yaml](../../examples/platform-managed-parameter-contexts-kind-values.yaml)
- [platform-managed-parameter-contexts-update-kind-values.yaml](../../examples/platform-managed-parameter-contexts-update-kind-values.yaml)

Compose it with:

```bash
helm template test charts/nifi-platform \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-parameter-contexts-values.yaml
```

Focused runtime proof:

```bash
make kind-parameter-contexts-runtime-fast-e2e
```

## What This Feature Does Not Do

- create generic Controller Services or generic runtime objects
- create or reconcile NiFi Parameter Providers
- assign Parameter Contexts arbitrarily across the flow graph
- manage arbitrary processor, connection, or process-group mutation
- provide controller-managed continuous flow sync
