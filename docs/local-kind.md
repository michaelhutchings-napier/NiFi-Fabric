# Local Kind Workflow

This repository now includes concrete local workflows for:

- a standalone NiFi 2 chart install
- a managed-mode install with the thin `OnDelete` rollout controller

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

## Managed Rollout Behavior

The current managed slice does exactly this:

1. detect StatefulSet template or revision drift in managed `OnDelete` mode
2. wait for all target pods to be `Ready`
3. wait for the documented per-pod NiFi health gate to pass for multiple consecutive polls
4. delete the highest remaining ordinal in the current revision set
5. wait for the replacement pod to become `Ready`
6. wait for the full cluster to converge again
7. repeat until all ordinals have been replaced

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
- The kind-focused standalone example leaves `nifi.security.autoreload.enabled=false` for now; cert rotation policy is still a later slice.
- The checker uses the exported `ca.crt` rather than `curl -k`.
- Managed mode currently coordinates only template or revision drift rollouts.
- Config drift triggers, cert drift triggers, offload or disconnect sequencing, and hibernation are still intentionally deferred.
