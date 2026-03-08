# Local Kind Workflow

This repository now includes concrete local workflows for:

- a standalone NiFi 2 chart install
- a managed-mode install with the thin `OnDelete` rollout controller
- watched config and TLS drift verification against that managed controller
- managed hibernation and restore verification against that same controller

## What This Flow Does

- creates a kind cluster
- creates a PKCS12 TLS Secret and a single-user auth Secret
- installs either the standalone or managed chart path
- validates the cluster with a repeatable health check that distinguishes Kubernetes readiness from NiFi convergence
- optionally builds and deploys the controller inside kind for managed rollout verification

This flow is intentionally local-development oriented. It is not a production deployment guide.

## Prerequisites

- Docker
- kind
- kubectl
- Helm 3
- openssl
- python3
- Go

`keytool` is optional. If it is not installed locally, `hack/create-kind-secrets.sh` runs `keytool` in a disposable `apache/nifi:2.0.0` container.

## Standalone Commands

```bash
make kind-up
make kind-secrets
make helm-install-standalone
make kind-health
```

The health check is implemented in `hack/check-nifi-health.sh`. It polls until the cluster is healthy or a timeout is reached.

## What The Health Check Proves

The script reports three separate stages:

1. `pods ready`
   - Kubernetes sees all target NiFi pods as `Ready=True`.
2. `secured API reachable`
   - each pod can mint a token against its own HTTPS endpoint using the configured auth Secret
3. `cluster converged`
   - each pod's own `flow/cluster/summary` reports:
     - `clustered=true`
     - `connectedToCluster=true`
     - `connectedNodeCount == expected replicas`
     - `totalNodeCount == expected replicas`

The script requires the convergence stage to pass for three consecutive polls before it exits successfully.

This distinction matters because a fresh install can reach `Ready=True` before NiFi agrees that the cluster is fully connected.

## Why The Check Uses Pod DNS Instead Of The Service

The authoritative convergence check should use direct pod DNS names behind the headless Service:

- `https://nifi-0.nifi-headless.nifi.svc.cluster.local:8443`
- `https://nifi-1.nifi-headless.nifi.svc.cluster.local:8443`
- `https://nifi-2.nifi-headless.nifi.svc.cluster.local:8443`

This avoids two false assumptions:

- a ClusterIP Service hides which node view is being queried
- a token minted on one node should not be assumed to work on every other node

The local checker acquires a token from each pod and then queries that same pod's `flow/cluster/summary`.

## Expected Startup Timeline

On one clean 3-node kind install using `examples/standalone/values.yaml`, the measured timeline was:

- `+116s`: all three pods became `Ready`
- `+116s`: all three secured APIs were reachable
- `+134s`: all three pods reported `3 / 3` connected nodes
- `+160s`: the convergence signal stayed healthy for three consecutive polls

Use this as an observation baseline, not a strict guarantee. Kind performance, local CPU, and image caching can shift the timings.

The important operational behavior is:

- pods can be `Ready` before the cluster is converged
- secured API reachability can appear before `Ready=True`
- future controller logic must wait through this observation window instead of treating the first successful API call as a rollout gate

## Observation Window And Fallback Signal

If the first cluster summary result is not fully converged after install:

- keep polling with backoff instead of treating it as a hard failure
- treat `pods ready` plus `secured API reachable` as a diagnostic fallback only
- do not treat the fallback signal as sufficient for restart, upgrade, or hibernation orchestration

For local validation, the default checker settings are:

- timeout: `600s`
- interval: `10s`
- stable polls required: `3`

You can override them when needed:

```bash
bash hack/check-nifi-health.sh \
  --namespace nifi \
  --statefulset nifi \
  --auth-secret nifi-auth \
  --timeout 900 \
  --interval 5 \
  --stable-polls 3
```

## Managed Mode Commands

Managed mode needs the controller running inside the kind cluster so it can reach direct pod DNS names.

The local image flow is intentionally simple:

- `make docker-build-controller` builds `bin/manager` with the host Go toolchain
- the Docker image uses a `scratch` runtime image and does not need to pull a base image for the local dev path

Exact commands:

```bash
make kind-up
make kind-secrets
make install-crd
make docker-build-controller
make kind-load-controller
make deploy-controller
kubectl -n nifi-system rollout status deployment/nifi2-platform-controller-manager --timeout=5m
make helm-install-managed
make apply-managed
make kind-health
```

To trigger a harmless template drift and watch the controller coordinate a rollout:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/managed/values.yaml \
  --reuse-values \
  --set-string podAnnotations.rolloutNonce=$(date +%s)
```

Useful watch commands while the rollout is running:

```bash
kubectl -n nifi get sts nifi \
  -o custom-columns=NAME:.metadata.name,CURRENT:.status.currentRevision,UPDATE:.status.updateRevision,CURRENTREPLICAS:.status.currentReplicas,UPDATED:.status.updatedReplicas,READY:.status.readyReplicas \
  --watch
```

```bash
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,REV:.metadata.labels.controller-revision-hash,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready \
  --watch
```

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
```

Final verification after the rollout settles:

```bash
make kind-health
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}'
```

## Managed Config Drift Verification

The managed example CR watches:

- `ConfigMap/nifi-config` as config input
- `Secret/nifi-tls` as certificate input

Config drift continues to use the managed rollout path. TLS drift uses either observation or that same rollout path depending on policy.

Exact commands:

```bash
make kind-config-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedConfigHash}{"\n"}{.status.rollout.trigger}{"\n"}{.status.rollout.targetConfigHash}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,CREATED:.metadata.creationTimestamp,READY:.status.containerStatuses[0].ready \
  --watch
```

Expected behavior:

- `ConditionProgressing=True` while the controller applies watched config drift
- `status.rollout.trigger=ConfigDrift`
- pods are deleted highest ordinal first, one at a time
- `status.observedConfigHash` updates only after the cluster is healthy again

Final verification:

```bash
make kind-health
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedConfigHash}{"\n"}{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}'
```

## Managed TLS Drift Verification

TLS drift now follows an explicit policy path:

- stable TLS content drift enters a short autoreload observation window
- stable content drift resolves without restart if the cluster stays healthy
- `spec.restartPolicy.tlsDrift=AlwaysRestart` skips observation and starts a rollout immediately
- TLS secret ref, mount path, or password-key changes are always restart-required

The built-in observation window is currently `30s`.

Exact commands:

```bash
make kind-tls-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedCertificateHash}{"\n"}{.status.observedTLSConfigurationHash}{"\n"}{.status.tls.observationStartedAt}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready
```

Expected behavior:

- `ConditionProgressing=True` with `TLSAutoreloadObserving` during the observation window
- `status.tls.observationStartedAt` is populated while the controller waits to see whether autoreload settles cleanly
- `status.observedCertificateHash` advances only after the controller considers the TLS state reconciled
- no pod should have a deletion timestamp

Final verification after the observation window:

```bash
make kind-health
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedCertificateHash}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
```

## Managed Restart-Required TLS Drift Verification

This flow changes the TLS mount path in the managed chart values. That is a material TLS configuration change, so the controller should skip observation and trigger the existing managed rollout path.

Exact commands:

```bash
make kind-tls-config-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.rollout.targetCertificateHash}{"\n"}{.status.rollout.targetTLSConfigurationHash}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready \
  --watch
```

Expected behavior:

- `status.rollout.trigger=TLSDrift`
- `ConditionProgressing=True` with a TLS-rollout reason
- pods are deleted highest ordinal first, one at a time
- `status.observedCertificateHash` and `status.observedTLSConfigurationHash` advance only after the rollout completes and the cluster is healthy again

Final verification:

```bash
make kind-health
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedCertificateHash}{"\n"}{.status.observedTLSConfigurationHash}{"\n"}{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}'
```

## Managed Rollout Behavior

The current managed slice does exactly this:

1. detect StatefulSet template or revision drift in managed `OnDelete` mode
2. detect watched non-TLS config drift from `spec.restartTriggers`
3. detect watched TLS drift and decide whether to observe autoreload or require rollout
4. wait for all target pods to be `Ready`
5. wait for the documented per-pod NiFi health gate to pass for multiple consecutive polls
6. delete the highest remaining ordinal in the current revision set
7. wait for the replacement pod to become `Ready`
8. wait for the full cluster to converge again
9. repeat until all ordinals have been replaced

On one clean kind run, the observed delete order was:

- `nifi-2`
- `nifi-1`
- `nifi-0`

Implementation notes from that run:

- the pod replacement sequence was correct and reproducible
- StatefulSet `currentRevision` lagged briefly after the pods had already converged
- the controller now treats the rollout as complete once all pods are on the target revision and the health gate is satisfied, even if that status field lags for a short period

## Expected Secrets

`make kind-secrets` calls `hack/create-kind-secrets.sh` and creates:

- `Secret/nifi-tls`
- `Secret/nifi-auth`

The TLS Secret contains:

- `keystore.p12`
- `truststore.p12`
- `ca.crt`
- `keystorePassword`
- `truststorePassword`
- `sensitivePropsKey`

The auth Secret contains:

- `username`
- `password`

## Notes

- The helper script uses a self-signed CA and a server certificate valid for the chart Service and headless Service DNS names.
- The helper script creates both PKCS12 stores. If the workstation does not have `keytool`, it runs `keytool` in a disposable `apache/nifi:2.0.0` container.
- The health checker executes `curl` inside each NiFi pod so the TLS hostname and NiFi node identity stay aligned.
- The chart default still leaves `nifi.security.autoreload.enabled=false` for the standalone path, but the managed example used for TLS policy verification enables autoreload explicitly.
- The checker uses the exported `ca.crt` rather than `curl -k`.
- Managed mode currently coordinates template, revision, and watched non-TLS config drift rollouts through the same `OnDelete` path.
- Stable TLS content drift observes autoreload first and can reconcile without restart.
- Material TLS configuration changes and restart-required TLS policy decisions use the same managed `OnDelete` path.
- Managed hibernation is implemented as a direct scale-to-zero and restore flow.
- Offload or disconnect sequencing before restart or scale-down is still intentionally deferred.

## Managed Hibernation And Restore Verification

The current hibernation slice is intentionally small:

- it captures `status.hibernation.lastRunningReplicas` before the first scale-down below the running size
- it scales the target `StatefulSet` directly to `0`
- it does not delete PVCs
- it restores back to `status.hibernation.lastRunningReplicas`
- if that status field is absent, it falls back to `1` replica

Exact commands:

```bash
make kind-hibernate
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.hibernation.lastRunningReplicas}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get sts nifi -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}'
kubectl -n nifi get pvc
kubectl -n nifi get pods
```

Expected behavior:

- `status.hibernation.lastRunningReplicas` captures the pre-hibernation running size
- the target `StatefulSet.spec.replicas` becomes `0`
- PVCs remain present
- `ConditionHibernated=True` appears only after pods are fully gone

Restore commands:

```bash
make kind-restore
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.hibernation.lastRunningReplicas}{"\n"}{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}'
kubectl -n nifi get sts nifi -o jsonpath='{.spec.replicas}{"\n"}{.status.readyReplicas}{"\n"}' --watch
make kind-health
```

Expected behavior:

- the target `StatefulSet.spec.replicas` returns to the recorded running size
- `ConditionProgressing=True` with a restore reason remains set until health is stable again
- the controller reports success only after the same per-pod NiFi convergence gate passes
