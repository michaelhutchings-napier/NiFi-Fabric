# OpenShift Readiness Guide

This guide prepares `NiFi-Fabric` for future OpenShift evaluation. It is not an OpenShift validation report.

## Current Status

Proven on kind:

- standalone Helm install
- managed-mode install with the thin controller
- per-pod NiFi health gate
- managed revision rollout
- config drift rollout
- TLS observe-only handling
- restart-required TLS rollout
- hibernation and restore
- focused cert-manager evaluator flow

Prepared for OpenShift:

- OpenShift-oriented Helm values overlays in `examples/openshift/`
- chart-level Route rendering with passthrough termination as the first readiness option
- SCC, security-context, storage, Route, and registry assumptions documented here
- managed-mode install steps using the existing chart, controller, and `NiFiCluster`

Not yet validated on OpenShift:

- runtime behavior on a real OpenShift cluster
- SCC compatibility of the current container image and volume permissions
- Route behavior for external NiFi access
- storage behavior on a real OpenShift StorageClass
- cert-manager behavior on OpenShift

## Prerequisites

- an OpenShift cluster with support for StatefulSets, PVCs, Routes, RBAC, and the route API
- `oc`, `kubectl`, `helm`, `curl`, `jq`, and `openssl`
- an image registry reachable from the cluster for the controller image
- outbound image pull access for `apache/nifi:2.0.0`, or a mirrored equivalent
- either:
  - an external TLS Secret plus auth Secret
  - or a separately installed cert-manager and issuer flow that matches the existing chart contract

## Proven On Kind Vs Prepared For OpenShift

Kind is still the only proven runtime in this repository.

OpenShift readiness now means:

- the chart can render OpenShift-specific values overlays
- the chart can render a Route
- the docs spell out SCC and storage assumptions up front
- the managed-mode installation path is documented

It does not mean:

- Route exposure has been proven end to end
- the current security contexts are known-good on restricted SCCs
- any OpenShift ingress, storage, or cert-manager path has actually been validated

## SCC And Security Context Assumptions

The current default chart values are kind-oriented:

- `podSecurityContext.fsGroup: 1000`
- `securityContext.runAsUser: 1000`
- `securityContext.runAsGroup: 1000`
- `securityContext.runAsNonRoot: true`

That may conflict with restricted OpenShift SCC behavior, where an arbitrary non-root UID is often assigned.

Prepared OpenShift starting point:

- remove the fixed `runAsUser`, `runAsGroup`, and `fsGroup` values
- keep `runAsNonRoot: true`

That is what the OpenShift example overlays do today.

Still unknown until a real cluster is tested:

- whether the NiFi image and mounted repository paths are fully compatible with the target SCC
- whether an additional SCC, UID range adjustment, or image filesystem permission fix is needed

## Storage Assumptions

The chart still creates four repository PVCs:

- `database_repository`
- `flowfile_repository`
- `content_repository`
- `provenance_repository`

Prepared OpenShift assumption:

- start with the cluster's default `ReadWriteOnce` StorageClass, or set a platform-specific class explicitly in the OpenShift overlay before install

The OpenShift example overlays leave `persistence.storageClassName` empty on purpose because the right value is cluster-specific.

Not yet validated:

- PVC provisioning and attach timing
- scale-down and restore behavior on real OpenShift storage
- volume permission behavior under the target SCC

## Route Assumptions

Prepared OpenShift readiness model:

1. start with internal `ClusterIP` access and the existing per-pod health gate
2. add a passthrough Route as the first OpenShift exposure option
3. treat reencrypt as a future alternative, not the default readiness path

Why passthrough first:

- NiFi already terminates TLS itself
- the current chart and controller assume end-to-end NiFi TLS
- passthrough avoids introducing a second TLS termination point into the first OpenShift readiness pass

Current chart support:

- `openshift.route.enabled`
- `openshift.route.host`
- `openshift.route.annotations`
- `openshift.route.tls.termination`
- `openshift.route.tls.insecureEdgeTerminationPolicy`

Prepared default:

- `termination: passthrough`

Not yet validated:

- Route host behavior for the NiFi UI
- whether additional `nifi.web.proxy.host` handling is needed for real external access
- reencrypt Route behavior

## Image Registry And Pull Assumptions

Current assumptions:

- the chart defaults to `apache/nifi:2.0.0`
- cluster nodes must be able to pull that image directly, or you must override `image.repository` and `image.tag`
- the controller deployment manifest in `config/manager/manager.yaml` still defaults to the local dev image `nifi-fabric-controller:dev` with `imagePullPolicy: Never`

For OpenShift evaluation, build and push the controller image to a cluster-reachable registry:

```bash
export CONTROLLER_IMAGE=<your-registry>/nifi-fabric-controller:alpha
docker build -t "${CONTROLLER_IMAGE}" .
docker push "${CONTROLLER_IMAGE}"
```

Then patch the deployed controller:

```bash
oc -n nifi-system set image deployment/nifi-fabric-controller-manager manager="${CONTROLLER_IMAGE}"
oc -n nifi-system patch deployment nifi-fabric-controller-manager \
  --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
```

`NiFi-Fabric` does not yet expose a first-class `imagePullSecrets` value in the chart or controller deployment. Use images the cluster can already pull, or patch manifests in your evaluation environment.

## External Secret Vs Cert-Manager

### External Secret Mode

`tls.mode=externalSecret` remains the baseline OpenShift path.

Before install, create:

- `Secret/nifi-tls`
- `Secret/nifi-auth`

The TLS Secret still needs:

- `ca.crt`
- `keystore.p12`
- `truststore.p12`
- stable keystore and truststore password keys
- `sensitivePropsKey` unless you provide a separate secret reference

### Cert-Manager Mode

`tls.mode=certManager` is prepared for OpenShift, not validated there.

Current assumptions remain unchanged:

- cert-manager is a separate cluster dependency
- the issuer writes a stable Secret name
- the Secret includes `ca.crt`
- PKCS12 password refs stay stable
- `nifi.sensitive.props.key` stays stable

You can compose:

- `examples/openshift/managed-values.yaml`
- `examples/cert-manager-values.yaml`

But that should still be treated as OpenShift-prepared only until a real cluster run is recorded.

## OpenShift-Oriented Example Values

Starting overlays:

- `examples/openshift/standalone-values.yaml`
- `examples/openshift/managed-values.yaml`

These overlays currently:

- keep the Service internal as `ClusterIP`
- relax the fixed kind-style UID and GID settings
- render a passthrough Route
- leave StorageClass selection explicit to the cluster operator

## Managed-Mode Install Steps

Once an OpenShift cluster is available, start with the managed path because it exercises the full hybrid model.

1. Log in and target the project.

```bash
oc login <your-api-server>
oc new-project nifi --skip-config-write || oc project nifi
oc new-project nifi-system --skip-config-write || oc project nifi-system
```

2. Prepare TLS and auth prerequisites.

External Secret mode:

```bash
oc -n nifi apply -f <your-nifi-tls-secret.yaml>
oc -n nifi apply -f <your-nifi-auth-secret.yaml>
```

Cert-manager mode:

- install cert-manager separately
- prepare the issuer and password secrets expected by the current chart contract

3. Install the CRD.

```bash
oc apply -f config/crd/bases/platform.nifi.io_nificlusters.yaml
```

4. Install the controller.

```bash
oc apply -f config/rbac/service_account.yaml
oc apply -f config/rbac/role.yaml
oc apply -f config/rbac/role_binding.yaml
oc apply -f config/manager/manager.yaml
oc -n nifi-system set image deployment/nifi-fabric-controller-manager manager="${CONTROLLER_IMAGE}"
oc -n nifi-system patch deployment nifi-fabric-controller-manager \
  --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
oc -n nifi-system rollout status deployment/nifi-fabric-controller-manager --timeout=5m
```

5. Install the chart with the OpenShift managed overlay.

External Secret mode:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/openshift/managed-values.yaml
```

Cert-manager mode:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/openshift/managed-values.yaml \
  -f examples/cert-manager-values.yaml
```

6. Apply the `NiFiCluster`.

```bash
oc apply -f examples/managed/nificluster.yaml
```

7. Verify internal health before depending on the Route.

```bash
bash hack/check-nifi-health.sh --namespace nifi --statefulset nifi --auth-secret nifi-auth
```

8. Inspect the Route only after the internal health gate is passing.

```bash
oc -n nifi get route nifi -o yaml
```

## First Things To Validate On Real OpenShift

The first real OpenShift evaluation should answer:

- does the current NiFi image run successfully under the target SCC without fixed UID or GID settings
- do the repository PVCs provision and remain writable on the chosen StorageClass
- does the per-pod DNS health gate behave the same way on OpenShift networking
- does the managed `OnDelete` rollout model behave the same way with OpenShift StatefulSet timing
- does the passthrough Route work for external HTTPS access, or is extra proxy-host handling required
- does cert-manager renewal preserve the current autoreload-first behavior without unexpected restart

Until those answers come from a real OpenShift run, treat this guide as readiness guidance only.
