# Operations Playbooks

These playbooks are for planned changes.

Use [Starter runbooks](runbooks.md) when something is already broken or degraded.
Use this page when you are about to make a supported change and want a stable,
repeatable operator workflow first.

These playbooks apply across the supported NiFi `2.x` line. They document the
standard NiFi-Fabric operating model rather than introducing per-version
procedures.

The example commands on this page assume the default release name `nifi` in the
`nifi` namespace. If your release name differs, adjust the generated object
names such as `nifi-versioned-flow-imports` and `nifi-nifidataflows-status`.

## Platform Upgrade Playbook

Use this playbook for:

- chart upgrades
- controller upgrades
- supported NiFi image tag changes
- values changes that change the NiFi pod template or otherwise trigger a managed rollout

### Before You Start

- confirm the target state is still within the supported product surface in [Compatibility](../compatibility.md)
- make the declarative change explicit in Git or the values source your team uses for GitOps
- export the current control-plane intent before the change:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

- confirm the cluster is not already mid-lifecycle before you change it:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{"\n"}{.status.lastOperation.type}{" "}{.status.lastOperation.phase}{" "}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods -o wide
kubectl -n nifi-system get deployment,pods
```

- avoid stacking unrelated changes into the same upgrade when you want a clean support signal
- do not manually delete pods or scale the NiFi `StatefulSet`

### Change Procedure

Apply the new desired state through Helm or your GitOps controller.

Example:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  --set-string nifi.image.tag=2.8.0
```

If the change also upgrades the controller, let the controller deployment settle
first and then watch the managed NiFi rollout.

### What To Watch

Check the Helm release:

```bash
helm -n nifi status nifi
```

Watch the managed rollout state:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods -w
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Expected behavior on the standard managed path:

- the rendered workload template changes first
- the `StatefulSet` stays on `OnDelete`
- the controller performs one destructive step at a time
- highest ordinal is restarted first by default
- the controller waits for pod readiness and NiFi rejoin before moving on

### Success Checks

Confirm:

- the Helm release is at the intended revision
- the NiFi pods are Ready at the intended image and template revision
- the `NiFiCluster` no longer shows a blocked or failed rollout state
- controller logs do not show a stuck node-preparation or reconnect loop

Useful checks:

```bash
kubectl -n nifi get nificluster,statefulset,pods
kubectl -n nifi describe nificluster nifi
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

### If The Upgrade Blocks Or Fails

- stop issuing more upgrades until you understand the current lifecycle state
- inspect `status.rollout.trigger`, `status.nodeOperation`, and controller logs first
- if node preparation is stuck, treat it as a rollout problem, not as a reason to delete more pods manually
- if the cluster is degraded, use [Starter runbooks](runbooks.md) before attempting another declarative change

### Rollback Boundary

Rollback stays declarative:

- restore the previous chart version, values, and image tag in Git or your Helm input
- apply that previous desired state with the normal Helm or GitOps path
- let the controller reconcile the rollback through the same safety model

Do not treat `kubectl delete pod` as rollback.

## Flow Version-Change Playbook

Use this playbook for:

- changing a declared `versionedFlowImports.imports[].version`
- changing the selected flow source version through the optional `NiFiDataflow` bridge
- changing the bounded rollout policy for a managed flow version change

This playbook does not cover arbitrary manual UI edits to the same managed
target.

### Before You Start

- confirm the target import is within the product-owned scope described in [Flows](../manage/flows.md)
- confirm the selected Flow Registry Client, bucket, flow, and version are still reachable
- decide whether the change should use `Replace` or `DrainAndReplace`
- avoid manual edits to the same managed imported process group before and during the change

For the optional `NiFiDataflow` bridge path, also confirm the bridge is enabled
and the managed runtime status projection is healthy:

```bash
kubectl -n nifi get configmap nifi-nifidataflows-status -o yaml
kubectl -n nifi get nifidataflows
```

### Change Procedure

Update the declared version and apply it through the same declarative path you
already use.

Platform chart example:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-versioned-flow-import-values.yaml
```

Optional `NiFiDataflow` bridge example:

```bash
kubectl apply -f config/samples/platform_v1alpha1_nifidataflow.yaml
```

Do not restart pod `-0` manually as part of a normal declared version change.

### What To Watch

Watch the runtime-owned import status:

```bash
kubectl -n nifi get configmap nifi-versioned-flow-imports -o yaml
kubectl -n nifi logs nifi-0 -c nifi --tail=200
```

If the optional `NiFiDataflow` bridge is enabled, also watch:

```bash
kubectl -n nifi get configmap nifi-nifidataflows-status -o yaml
kubectl -n nifi get nifidataflows -o yaml
kubectl -n nifi get events --field-selector involvedObject.kind=NiFiDataflow --sort-by=.lastTimestamp
```

Expected behavior on the bounded managed path:

- the live reconcile loop runs on pod `-0`
- a normal declared version change reconciles without replacing pod `-0`
- `DrainAndReplace` waits for the owned target to quiesce before switching versions
- unsupported drift or same-name operator-owned conflicts are reported as `blocked` rather than silently overwritten

### Success Checks

Confirm:

- the expected version is reflected in the runtime status or `NiFiDataflow.status`
- the managed target remains owned and healthy
- the change did not produce `Blocked` or `Failed` runtime status

Useful checks for the standard chart-managed path:

```bash
kubectl -n nifi get configmap nifi-versioned-flow-imports -o yaml
kubectl -n nifi logs nifi-0 -c nifi --tail=200
```

Useful checks for the optional `NiFiDataflow` bridge path:

```bash
kubectl -n nifi get configmap nifi-nifidataflows-status -o yaml
kubectl -n nifi get nifidataflows -o yaml
```

### If The Change Blocks Or Fails

- if the runtime reports `blocked`, inspect ownership conflicts, missing registry content, or unsupported manual drift first
- if `DrainAndReplace` times out, inspect running descendants and queue activity before retrying
- do not assume the right fix is to recreate the managed target or restart pod `-0`
- if the controller bridge path reports unreadable runtime status, fix that projection problem before trusting `NiFiDataflow.status`

### Rollback Boundary

Rollback also stays declarative:

- restore the previously declared flow version
- apply the prior declaration through Helm or `kubectl apply`
- let the bounded runtime path reconcile back to that prior version

If ownership was lost or manual drift widened beyond the supported scope, fix
that explicit operator-owned problem first rather than expecting auto-adoption.
