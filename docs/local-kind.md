# Local Kind Workflow

This repository now includes concrete local workflows for:

- a standalone NiFi 2 chart install
- a managed-mode install with the thin `OnDelete` rollout controller
- watched config and TLS drift verification against that managed controller
- managed hibernation and restore verification against that same controller
- focused OIDC auth runtime validation on kind
- focused LDAP auth runtime validation on kind

## What This Flow Does

- creates a kind cluster
- creates a PKCS12 TLS Secret and a single-user auth Secret
- installs either the standalone or managed chart path
- validates the cluster with a repeatable health check that distinguishes Kubernetes readiness from NiFi convergence
- optionally builds and deploys the controller inside kind for managed rollout verification

This flow is intentionally local-development oriented. It is not a production deployment guide.

Recommended example files:

- standalone Helm values: [examples/standalone/values.yaml](../examples/standalone/values.yaml)
- managed Helm values: [examples/managed/values.yaml](../examples/managed/values.yaml)
- optional cert-manager TLS overlay: [examples/cert-manager-values.yaml](../examples/cert-manager-values.yaml)
- managed `NiFiCluster`: [examples/managed/nificluster.yaml](../examples/managed/nificluster.yaml)
- rollout trigger overlay: [examples/managed/rollout-trigger-values.yaml](../examples/managed/rollout-trigger-values.yaml)
- hibernation example: [examples/managed/nificluster-hibernated.yaml](../examples/managed/nificluster-hibernated.yaml)
- prepared OIDC auth overlay: [examples/oidc-values.yaml](../examples/oidc-values.yaml)
- prepared OIDC group-claims overlay: [examples/oidc-group-claims-values.yaml](../examples/oidc-group-claims-values.yaml)
- prepared OIDC external URL overlay: [examples/oidc-external-url-values.yaml](../examples/oidc-external-url-values.yaml)
- prepared LDAP overlay: [examples/ldap-values.yaml](../examples/ldap-values.yaml)
- prepared GitHub Flow Registry Client overlay: [examples/github-flow-registry-values.yaml](../examples/github-flow-registry-values.yaml)
- prepared GitLab Flow Registry Client overlay: [examples/gitlab-flow-registry-values.yaml](../examples/gitlab-flow-registry-values.yaml)
- prepared Bitbucket Flow Registry Client overlay: [examples/bitbucket-flow-registry-values.yaml](../examples/bitbucket-flow-registry-values.yaml)
- prepared Azure DevOps Flow Registry Client overlay: [examples/azure-devops-flow-registry-values.yaml](../examples/azure-devops-flow-registry-values.yaml)
- prepared ingress and proxy-host overlay: [examples/ingress-proxy-host-values.yaml](../examples/ingress-proxy-host-values.yaml)
- prepared OpenShift Route and proxy-host overlay: [examples/openshift/route-proxy-host-values.yaml](../examples/openshift/route-proxy-host-values.yaml)

## Authn And Authz Scope

The chart now splits authentication and authorization cleanly:

- one authentication mode at a time
- one matching authorization mode at a time
- no controller write-back
- no bidirectional identity sync
- no default per-user provisioning for OIDC
- prefer `authz.bootstrap.initialAdminGroup`; use `initialAdminIdentity` only as a fallback

Current supported pairs:

- `singleUser + fileManaged`
- `oidc + externalClaimGroups`
- `ldap + ldapSync`

What is proven on kind now:

- `singleUser + fileManaged` in the main alpha gate
- focused OIDC login wiring, group-claim prerequisites, Initial Admin Identity fallback bootstrap, and non-admin denial with `make kind-auth-oidc-e2e`
- focused LDAP login wiring, LDAP provider wiring, Initial Admin Identity bootstrap, and non-admin denial with `make kind-auth-ldap-e2e`

What is still only prepared:

- OIDC custom non-admin policy bindings from `authz.policies`
- LDAP broader group-policy seeding beyond the focused bootstrap path
- ingress-backed or Route-backed auth runtime behavior
- external Flow Registry Client runtime against GitHub, GitLab, Bitbucket, or Azure DevOps

Render-time validation now fails fast for:

- unsupported auth/authz pairings
- missing OIDC discovery, client, or group-claim settings
- missing LDAP provider settings
- missing enterprise admin bootstrap configuration
- OIDC external exposure without `web.proxyHosts`

### External URL And Proxy Host Guidance

NiFi still serves HTTPS itself. External access must preserve that model:

- ingress example: [examples/ingress-proxy-host-values.yaml](../examples/ingress-proxy-host-values.yaml)
- OIDC external URL example: [examples/oidc-external-url-values.yaml](../examples/oidc-external-url-values.yaml)
- OpenShift Route example: [examples/openshift/route-proxy-host-values.yaml](../examples/openshift/route-proxy-host-values.yaml)

For OIDC:

- the browser-facing HTTPS host must be present in `web.proxyHosts`
- the OIDC redirect will fail or loop if NiFi does not trust that public host
- token group names must match `authz.applicationGroups` exactly

For LDAP:

- login stays NiFi-native
- policy bindings still use NiFi group names in `authz.policies`
- if users reach NiFi through an ingress or Route, set `web.proxyHosts` to the public HTTPS host

Prepared render checks:

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/oidc-values.yaml \
  -f examples/oidc-group-claims-values.yaml \
  -f examples/oidc-external-url-values.yaml
```

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/ldap-values.yaml \
  -f examples/ingress-proxy-host-values.yaml
```

Prepared Flow Registry Client render checks:

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/github-flow-registry-values.yaml
```

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/gitlab-flow-registry-values.yaml
```

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/bitbucket-flow-registry-values.yaml
```

```bash
helm template nifi charts/nifi \
  -f examples/managed/values.yaml \
  -f examples/azure-devops-flow-registry-values.yaml
```

Flow Registry Client scope:

- classic NiFi Registry is not the preferred path in this repo
- Git-based Flow Registry Clients are the prepared direction
- the chart renders a validated in-pod catalog only
- it does not auto-create clients in NiFi
- there is no controller-managed flow import or synchronization

### Bootstrap And Break-Glass

Preferred bootstrap:

- `authz.bootstrap.initialAdminGroup`

Fallback bootstrap:

- `authz.bootstrap.initialAdminIdentity`

If an OIDC or LDAP overlay locks you out, recover by rendering the last known-good single-user baseline again:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  --reset-values \
  -f examples/managed/values.yaml
make kind-health
```

Use the standalone baseline instead when you are not running managed mode:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  --reset-values \
  -f examples/standalone/values.yaml
make kind-health
```

## Private Alpha Workflow

The repo now includes a single fresh-kind entrypoint:

```bash
make kind-alpha-e2e
```

The workflow currently:

- creates a fresh kind cluster
- preloads the NiFi runtime image into the kind node
- installs the controller and managed chart
- runs the per-pod health gate
- exercises managed rollout, config drift, TLS observe-only, TLS restart-required, hibernation, and restore
- checks controller metrics and Kubernetes events
- fails fast and dumps diagnostics on the first failing stage

The repo also includes phase-level fresh-kind targets:

```bash
make kind-e2e-rollout
make kind-e2e-config-drift
make kind-e2e-tls
make kind-e2e-hibernate
```

Each target provisions a fresh cluster and runs only the minimum lifecycle slice needed for that phase.

There is also a focused cert-manager evaluator path:

```bash
make kind-cert-manager-e2e
```

Focused auth evaluator paths:

```bash
make kind-auth-oidc-e2e
make kind-auth-ldap-e2e
```

`make kind-auth-oidc-e2e` bootstraps Keycloak, deploys NiFi in `oidc + externalClaimGroups`, proves OIDC login wiring, proves exact group-name seeding prerequisites, and uses the documented `Initial Admin Identity` fallback for the first admin path.

`make kind-auth-ldap-e2e` bootstraps LDAP, deploys NiFi in `ldap + ldapSync`, proves LDAP login and provider wiring, and uses the documented `Initial Admin Identity` bootstrap path.

## Prerequisites

- Docker
- kind
- kubectl
- Helm 3
- curl
- jq
- openssl
- python3
- Go

`keytool` is optional. If it is not installed locally, `hack/create-kind-secrets.sh` falls back to a short-lived in-cluster `keytool` step using the already-loaded NiFi image.

## One-Command Evaluator Installs

Standalone:

```bash
make install-standalone
```

Managed:

```bash
make install-managed
```

Managed with cert-manager:

```bash
make install-managed-cert-manager
```

Each installer:

- creates or reuses the local kind cluster
- creates the required namespaces
- installs CRDs, controller, and `NiFiCluster` only when that mode needs them
- installs the chart
- prints the next health and debug commands
- checks `kind`, `kubectl`, `helm`, and `docker` first
- checks `cmctl` only for the cert-manager installer path
- prints the most useful failure commands for events, controller logs, `NiFiCluster` status, and controller metrics

## Standalone Commands

One-command path:

```bash
make install-standalone
```

Verbose equivalent:

```bash
make kind-up
make kind-load-nifi-image
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

One-command path:

```bash
make install-managed
```

Verbose equivalent:

```bash
make kind-up
make kind-load-nifi-image
make kind-secrets
make install-crd
make docker-build-controller
make kind-load-controller
make deploy-controller
kubectl -n nifi-system rollout status deployment/nifi-fabric-controller-manager --timeout=5m
make helm-install-managed
make apply-managed
make kind-health
```

The full alpha path wraps these commands plus the drift and hibernation checks:

```bash
make kind-alpha-e2e
```

For CI or local artifact capture:

```bash
ARTIFACT_DIR=$PWD/artifacts/alpha-debug make kind-e2e-tls
```

To trigger a harmless template drift and watch the controller coordinate a rollout:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  -f examples/managed/values.yaml \
  -f examples/managed/rollout-trigger-values.yaml \
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

Most useful operator status command:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
```

Useful live debug commands:

```bash
kubectl -n nifi get sts nifi -o custom-columns=NAME:.metadata.name,SPEC:.spec.replicas,READY:.status.readyReplicas,CURRENT:.status.currentRevision,UPDATE:.status.updateRevision
kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{" "}{.status.nodeOperation.nodeId}{"\n"}{.status.tls.observationStartedAt}{"\n"}{.status.hibernation.lastRunningReplicas}{"\n"}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi get events --field-selector involvedObject.kind=NiFiCluster,involvedObject.name=nifi --sort-by=.lastTimestamp
kubectl -n nifi-system logs deployment/nifi-fabric-controller-manager --tail=200
kubectl -n nifi get events --sort-by=.lastTimestamp | tail -n 50
```

Controller metrics quick check:

```bash
kubectl -n nifi-system port-forward deployment/nifi-fabric-controller-manager 18080:8080
curl --silent http://127.0.0.1:18080/metrics | rg 'nifi_platform_(lifecycle_transitions_total|rollouts_total|tls_actions_total|hibernation_operations_total|node_preparation_outcomes_total)'
```

Interpretation notes:

- `Progressing=True` with reason `PreparingNodeForRestart` or `PreparingNodeForHibernation` means the controller is still in the NiFi disconnect or offload sequence and has not taken the destructive Kubernetes step yet.
- `status.rollout.trigger` tells you whether the current managed restart was caused by StatefulSet revision drift, watched config drift, or TLS drift.
- `status.nodeOperation` is the best single place to see which pod and NiFi node are currently being prepared.
- `status.tls.observationStartedAt` plus `Progressing=TLSAutoreloadObserving` means the controller is intentionally waiting for NiFi autoreload before deciding whether restart is needed.
- `nifi_platform_node_preparation_outcomes_total` counts retry and timeout observations, not unique pods.

## Failure Diagnostics

On any alpha-phase failure, the workflow now dumps:

- `NiFiCluster` YAML and `describe`
- target `StatefulSet` YAML
- pod revision, readiness, UID, and deletion timestamps
- full `describe pods`
- controller logs
- `nifi` and `nifi-system` events

If `ARTIFACT_DIR` is set, those diagnostics are also written to files for CI upload.

The artifact bundle now includes concise `NiFiCluster` status, compact `StatefulSet` status, pod revision and UID state, recent events, controller logs, and a controller metrics snapshot in addition to the full YAML dumps.

## Known Limitations

- The workflow is a private-alpha confidence gate, not a production certification suite.
- The default alpha path still assumes pre-created TLS and auth Secrets.
- cert-manager installation and renewal are not part of `make kind-alpha-e2e`.
- cert-manager now has its own focused kind workflow instead of being folded into the alpha gate.
- `make kind-load-nifi-image` is part of the supported alpha path; if the chart image tag changes, update that helper to match.

## Optional Cert-Manager TLS Mode

The chart now supports:

- `tls.mode=externalSecret`
  - default
  - the existing `Secret/nifi-tls` contract stays unchanged
- `tls.mode=certManager`
  - Helm renders a cert-manager `Certificate`
  - cert-manager owns the TLS Secret contents
  - the controller still owns only TLS drift observation and restart decisions

This is intentionally not part of the automated alpha gate. Use it when cert-manager is already installed and you want the chart to manage `Certificate` resources without changing the controller model.

One-command path:

```bash
make install-managed-cert-manager
```

If you want the repo to set up cert-manager for you on a fresh kind cluster, use:

```bash
make kind-bootstrap-cert-manager
```

That bootstrap path:

- installs cert-manager from the official `jetstack/cert-manager` Helm chart
- waits for cert-manager controller, webhook, and cainjector readiness
- creates the evaluator `Issuer/nifi-selfsigned-bootstrap`
- creates `Certificate/nifi-root-ca`
- creates `ClusterIssuer/nifi-ca`

The focused end-to-end evaluator path then uses the same issuer contract:

```bash
make kind-cert-manager-e2e
```

That path installs cert-manager if needed, bootstraps the `nifi-ca` `ClusterIssuer`, deploys the managed chart with the cert-manager overlay, verifies renewal without restart, and then verifies a restart-required TLS config change.

The chart now defaults cert-manager mode to a non-empty certificate subject:

- `tls.certManager.commonName` defaults to `<release>.<namespace>.svc.cluster.local`
- NiFi derives node identity from the certificate subject, so cert-manager mode must not leave it empty
- override `tls.certManager.commonName` only when your issuer policy needs a different subject

Manual prerequisites:

- cert-manager already installed, or `make kind-bootstrap-cert-manager` already run
- an `Issuer` or `ClusterIssuer` that publishes `ca.crt`
- a stable Secret for the PKCS12 password and `nifi.sensitive.props.key`

Example parameter Secret:

```bash
kubectl create namespace nifi --dry-run=client -o yaml | kubectl apply -f -
kubectl -n nifi create secret generic nifi-tls-params \
  --from-literal=pkcs12Password=ChangeMeChangeMe1! \
  --from-literal=sensitivePropsKey=changeit-change-me \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n nifi create secret generic nifi-auth \
  --from-literal=username=admin \
  --from-literal=password=ChangeMeChangeMe1! \
  --dry-run=client -o yaml | kubectl apply -f -
```

Standalone install:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  --create-namespace \
  -f examples/standalone/values.yaml \
  -f examples/cert-manager-values.yaml
```

Managed install:

```bash
make kind-bootstrap-cert-manager
make kind-cert-manager-secrets
make install-crd
make docker-build-controller
make kind-load-controller
make deploy-controller
kubectl -n nifi-system rollout status deployment/nifi-fabric-controller-manager --timeout=5m
helm upgrade --install nifi charts/nifi \
  -n nifi \
  --create-namespace \
  -f examples/managed/values.yaml \
  -f examples/cert-manager-values.yaml
kubectl apply -f examples/managed/nificluster.yaml
make kind-health
```

Expected behavior:

- cert-manager renews the same TLS Secret name in place
- NiFi reads the same mount path and PKCS12 filenames
- stable Secret-content renewal is treated as ordinary TLS drift and enters the autoreload observation window
- the controller restarts only when policy or a material TLS wiring change requires it

Manual renewal verification:

```bash
cmctl renew -n nifi nifi
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedCertificateHash}{"\n"}{.status.tls.observationStartedAt}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready
make kind-health
```

For ordinary renewal with unchanged Secret name, mount path, and PKCS12 password refs, expect:

- `ConditionProgressing=True` with a TLS observation reason during the observation window
- no pod deletion timestamps
- `status.observedCertificateHash` to advance only after the controller accepts the renewed TLS material as steady state

Focused cert-manager evaluator path:

```bash
make kind-bootstrap-cert-manager
make kind-cert-manager-secrets
```

Minimal manual cert-manager evaluator path:

```bash
helm upgrade --install nifi charts/nifi \
  -n nifi \
  --create-namespace \
  -f examples/managed/values.yaml \
  -f examples/cert-manager-values.yaml
kubectl apply -f examples/managed/nificluster.yaml
make kind-health
```

Full focused cert-manager evaluator path:

```bash
make kind-cert-manager-e2e
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
6. prepare the highest remaining ordinal through NiFi `DISCONNECTING -> DISCONNECTED -> OFFLOADING -> OFFLOADED`
7. delete the highest remaining ordinal in the current revision set
8. wait for the replacement pod to become `Ready`
9. wait for the full cluster to converge again
10. repeat until all ordinals have been replaced

On one clean kind run, the observed delete order was:

- `nifi-2`
- `nifi-1`
- `nifi-0`

Implementation notes from that run:

- the pod replacement sequence was correct and reproducible
- StatefulSet `currentRevision` lagged briefly after the pods had already converged
- the controller now treats the rollout as complete once all pods are on the target revision and the health gate is satisfied, even if that status field lags for a short period
- `status.nodeOperation` is populated while the controller waits for NiFi to prepare the target node

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
- The helper script creates both PKCS12 stores. If the workstation does not have `keytool`, it falls back to a short-lived in-cluster `keytool` step so the truststore still contains Java trust anchors.
- The health checker executes `curl` inside each NiFi pod so the TLS hostname and NiFi node identity stay aligned.
- The chart default still leaves `nifi.security.autoreload.enabled=false` for the standalone path, but the managed example used for TLS policy verification enables autoreload explicitly.
- The checker uses the exported `ca.crt` rather than `curl -k`.
- Managed mode currently coordinates template, revision, and watched non-TLS config drift rollouts through the same `OnDelete` path.
- Stable TLS content drift observes autoreload first and can reconcile without restart.
- Material TLS configuration changes and restart-required TLS policy decisions use the same managed `OnDelete` path.
- Managed rollout and hibernation now use NiFi disconnect and offload sequencing before pod deletion or replica reduction.

## Managed Hibernation And Restore Verification

The current hibernation slice is intentionally small:

- it captures `status.hibernation.lastRunningReplicas` before the first scale-down below the running size
- it prepares the highest ordinal node through NiFi `DISCONNECTING -> DISCONNECTED -> OFFLOADING -> OFFLOADED`
- it reduces the target `StatefulSet` by one replica after that node is prepared
- it repeats until the cluster reaches zero replicas
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
- `status.nodeOperation` is populated while NiFi prepares the next highest ordinal node
- the target `StatefulSet.spec.replicas` decreases one step at a time
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
