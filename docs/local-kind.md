# Local Kind Workflow

This repository now includes a concrete local workflow for bringing up a minimal NiFi 2 cluster on kind using the standalone Helm chart.

## What This Flow Does

- creates a kind cluster
- creates a PKCS12 TLS Secret and a single-user auth Secret
- installs the standalone chart with kind-friendly values
- validates the cluster with a repeatable health check that distinguishes Kubernetes readiness from NiFi convergence

This flow is intentionally local-development oriented. It is not a production deployment guide.

## Prerequisites

- Docker
- kind
- kubectl
- Helm 3
- openssl
- python3

`keytool` is optional. If it is not installed locally, `hack/create-kind-secrets.sh` runs `keytool` in a disposable `apache/nifi:2.0.0` container.

## Exact Commands

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
- Managed mode still renders, but advanced controller-driven rollout behavior is not implemented yet.
