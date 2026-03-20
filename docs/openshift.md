# OpenShift Baseline Guide

This guide records the first runtime-proven OpenShift baseline for `NiFi-Fabric`.

OpenShift is still a target environment, not a separate architecture:

- `charts/nifi-platform` remains the primary install surface
- `charts/nifi` remains the reusable app chart
- the controller remains the sole executor of lifecycle and scale actions
- the first OpenShift proof is intentionally narrow and internal-first

## Current Status

Runtime-proven on a real OpenShift cluster:

- one-release managed install through `charts/nifi-platform`
- secure NiFi cluster startup and health on the internal `ClusterIP` path
- controller readiness and management of the chart-installed `NiFiCluster`
- baseline failure diagnostics for project or namespace state, `NiFiCluster` status, StatefulSet or pod state, PVC state, events, and controller logs

Still outside this first OpenShift proof:

- Route-backed exposure
- OIDC or LDAP browser login through an OpenShift Route
- cert-manager on OpenShift
- the standalone `charts/nifi` path on OpenShift
- restore, hibernation, autoscaling, and other deeper lifecycle flows re-run on OpenShift

## Exact Contract Proven

The current OpenShift baseline proves this contract and only this contract:

1. install with one Helm release through `charts/nifi-platform`
2. use the standard managed path with the controller and a chart-installed `NiFiCluster`
3. keep NiFi internal first with `Service.type=ClusterIP`
4. require secure NiFi startup and the existing internal health gate to pass
5. require the controller to become ready and report the managed `NiFiCluster` healthy through status conditions

The baseline does not require a Route and does not change the chart or controller split.

## OpenShift Overlays

Runtime-proven managed baseline composition:

- `examples/platform-managed-values.yaml`
- `examples/openshift/managed-values.yaml`

What the managed OpenShift overlay does:

- keeps the managed install on `charts/nifi-platform`
- removes fixed UID, GID, and `fsGroup` assumptions for both the controller and the NiFi workload
- keeps the NiFi Service internal as `ClusterIP`
- leaves `persistence.storageClassName` explicit to the cluster operator
- leaves Route enablement off in the baseline

Prepared but not yet runtime-proven OpenShift overlays:

- `examples/openshift/route-proxy-host-values.yaml`
- `examples/platform-managed-cert-manager-values.yaml`
- `examples/openshift/standalone-values.yaml`

## Prerequisites

- an OpenShift cluster with StatefulSets, PVCs, RBAC, and the route API available
- `oc`, `kubectl`, `helm`, `curl`, `jq`, `python3`, and `base64`
- a controller image reachable from the cluster
- a NiFi image reachable from the cluster
- a writable `ReadWriteOnce` StorageClass, either as the default or set explicitly in the OpenShift overlay
- these Secrets in the release namespace before install:
  - `Secret/nifi-auth`
  - `Secret/nifi-tls`

The default controller image in `examples/platform-managed-values.yaml` is a local dev image. For a real OpenShift run, override it to a registry image the cluster can pull.

## Focused Proof Command

The focused internal baseline command is:

```bash
CONTROLLER_IMAGE_REPOSITORY=<your-registry>/nifi-fabric-controller \
CONTROLLER_IMAGE_TAG=<tag> \
make openshift-platform-managed-proof
```

Default namespaces:

- release namespace: `nifi`
- controller namespace: `nifi-system`

Optional environment overrides:

- `NAMESPACE`
- `HELM_RELEASE`
- `CONTROLLER_NAMESPACE`
- `AUTH_SECRET`
- `NIFI_IMAGE_REPOSITORY`
- `NIFI_IMAGE_TAG`

What the proof command does:

- installs `charts/nifi-platform` with the standard managed values plus the OpenShift overlay
- waits for the controller Deployment to roll out
- verifies the chart-installed `NiFiCluster` and StatefulSet exist
- runs the secured per-pod health gate
- waits for `NiFiCluster` conditions `TargetResolved=True` and `Available=True`
- prints strong diagnostics automatically if the proof fails

## Manual Install Path

If you want to run the same baseline manually instead of the proof helper:

```bash
helm upgrade --install nifi charts/nifi-platform \
  --namespace nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/openshift/managed-values.yaml \
  --set controller.image.repository=<your-registry>/nifi-fabric-controller \
  --set controller.image.tag=<tag>
```

Then verify:

```bash
bash hack/check-nifi-health.sh --namespace nifi --statefulset nifi --auth-secret nifi-auth
kubectl -n nifi get nificluster nifi -o jsonpath='{range .status.conditions[*]}{.type}{"="}{.status}{"\n"}{end}'
kubectl -n nifi get statefulset,pod,pvc
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

If you use a different release name, the controller Deployment name follows the platform chart default `<release>-controller-manager`.

## Route and Auth Later

OpenShift Route work stays separate from the first baseline:

- compose `examples/openshift/route-proxy-host-values.yaml` only when you need external HTTPS access
- add `examples/oidc-values.yaml` and `examples/oidc-group-claims-values.yaml` for OIDC later
- add `examples/ldap-values.yaml` for LDAP later

Those compositions are still prepared only until a real Route-backed proof is recorded.

## Diagnostics

The OpenShift proof helper collects these on failure:

- current context, user, and selected project
- release and controller namespace or project state
- rendered `NiFiCluster` status and conditions
- StatefulSet, pod, PVC, and Route state
- namespace and controller events
- controller logs

This keeps the first OpenShift baseline support story explicit without adding new controllers, CRDs, or architecture forks.
