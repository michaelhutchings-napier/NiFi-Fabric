# nifi2-platform

`nifi2-platform` is a thin, modern platform layer for running Apache NiFi 2.x on Kubernetes.

## Private Alpha Quickstart

Primary local gate:

```bash
make kind-alpha-e2e
```

Phase reruns:

```bash
make kind-e2e-rollout
make kind-e2e-config-drift
make kind-e2e-tls
make kind-e2e-hibernate
```

CI entrypoints:

- GitHub Actions workflow `alpha-e2e`
- manual `workflow_dispatch` with target selection
- nightly scheduled full run

## Evaluator Prerequisites

Exact local prerequisites for the current private alpha:

- Docker with permission to run `kind` and `docker exec`
- kind
- kubectl
- Helm 3
- Go
- `openssl`
- `python3`

Optional:

- `keytool`
  - if it is missing, `hack/create-kind-secrets.sh` runs `keytool` in a disposable `apache/nifi:2.0.0` container

## Install Paths

Recommended evaluator entrypoints:

- Standalone quickstart
  - use when you only want Helm and a working NiFi 2 cluster
- Managed quickstart
  - use when you want the controller, `NiFiCluster`, and lifecycle orchestration
- Full private-alpha gate
  - use when you want the entire proven workflow on a fresh kind cluster

Example files are indexed in [examples/README.md](/home/michael/Work/nifi2-platform/examples/README.md).

## Standalone Quickstart

Exact commands:

```bash
make kind-up
make kind-load-nifi-image
make kind-secrets
make helm-install-standalone
make kind-health
```

Primary example:

- [examples/standalone/values.yaml](/home/michael/Work/nifi2-platform/examples/standalone/values.yaml)

## Managed Quickstart

Exact commands:

```bash
make kind-up
make kind-load-nifi-image
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

Primary examples:

- [examples/managed/values.yaml](/home/michael/Work/nifi2-platform/examples/managed/values.yaml)
- [examples/managed/nificluster.yaml](/home/michael/Work/nifi2-platform/examples/managed/nificluster.yaml)

## Full Alpha Gate

Exact command:

```bash
make kind-alpha-e2e
```

Phase-level reruns:

```bash
make kind-e2e-rollout
make kind-e2e-config-drift
make kind-e2e-tls
make kind-e2e-hibernate
```

The project is intentionally hybrid:

- Helm owns standard Kubernetes resources and NiFi configuration templating.
- A lightweight controller owns lifecycle and safety tasks Helm cannot perform safely.
- NiFi 2 native Kubernetes capabilities remain the source of truth for cluster coordination and shared state behavior.

The result should be easier to reason about than a large kitchen-sink operator, easier to run under GitOps, and easier to evolve as NiFi 2.x improves.

## Problem Statement

NiFi on Kubernetes needs two things at once:

- a clear, reviewable way to render ordinary Kubernetes resources
- a safe way to handle cert rotation, health-gated restarts, hibernation, and upgrade sequencing

Pure Helm is good at the first problem and weak at the second. Large operators can solve the second problem, but often by growing into broad APIs that duplicate application behavior, become hard to explain, and drift away from GitOps-friendly workflows.

NiFi 2 changes the design space. It already supports Kubernetes-native cluster coordination and shared state patterns, so a platform layer does not need to recreate those features. This project uses that fact to stay intentionally small.

## Vision

Build the thin NiFi 2 platform layer:

- NiFi 2.x only
- GitOps first
- AKS first, OpenShift-friendly second
- TLS and persistent storage by default
- boring Kubernetes patterns over clever abstractions
- one small operational CRD instead of a large configuration API

## Why Hybrid Helm + Controller

Helm is the right owner for:

- `StatefulSet`
- `Service` and headless `Service`
- `PersistentVolumeClaim`
- `ConfigMap`
- `Secret` references
- `PodDisruptionBudget`
- `ServiceMonitor`
- affinity, tolerations, topology spread, and other scheduling settings
- templated `nifi.properties` and related config files

The controller is still needed for:

- status conditions for operators and GitOps users
- watched Secret and ConfigMap hash detection
- safe rolling restart orchestration
- health-gated upgrade coordination
- hibernation and restore orchestration
- explicit offload and disconnect sequencing before restart or scale-down

NiFi native capabilities remain responsible for:

- Kubernetes-based cluster coordination
- shared state where configured
- cluster join and rejoin behavior
- TLS autoreload capability

## Operating Modes

| Mode | Installed components | Best for | Trade-off |
| --- | --- | --- | --- |
| Standalone chart | Helm chart only | teams that want plain Helm or simple GitOps | no controller-managed status, rollout safety, or hibernation |
| Managed mode | Helm chart + controller + `NiFiCluster` | teams that want safe orchestration and explicit status | requires a thin operational CR and documented controller ownership boundaries |

Managed mode is opt-in. The chart remains installable by itself.

## MVP Scope

The MVP includes:

- a standalone Helm chart for NiFi 2.x on Kubernetes
- an optional namespaced controller
- one namespaced CRD: `NiFiCluster`
- cert-manager integration assumptions
- `ServiceMonitor` support
- secure-by-default TLS-enabled clusters
- persistent volumes for NiFi repositories
- controlled config and cert drift handling
- health-gated rolling restarts and upgrades
- hibernation and restore to the prior running replica count
- explicit status conditions and events

## Non-Goals

This project does not aim to provide:

- Apache NiFi 1.x support
- NiFiKop compatibility or feature parity
- advanced flow deployment management
- user and access policy management CRDs
- NiFi Registry management CRDs
- backup and restore orchestration
- autoscaling logic
- multi-CRD modeling for every platform concern
- hidden automation that changes workloads without clear status or events

## Design Principles

- Prefer NiFi 2 native capabilities over custom controller logic.
- Prefer Helm for ordinary resources.
- Keep the controller thin, explicit, and testable.
- Keep the API boring and small.
- Make GitOps ownership boundaries obvious.
- Treat cert rotation and restart safety as first-class behavior.

## Recommended Repository Structure

After the design pack, the repository should grow into:

- `README.md`
- `TODO.md`
- `docs/`
- `charts/nifi/`
- `api/v1alpha1/`
- `internal/controller/`
- `internal/nifi/`
- `config/crd/`
- `config/rbac/`
- `config/samples/`
- `test/helm/`
- `test/envtest/`
- `test/e2e/`
- `examples/standalone/`
- `examples/managed/`

## Build Order

1. Finalize the design pack and API boundaries.
2. Build the standalone Helm chart and managed-mode chart switch.
3. Add the `NiFiCluster` CRD and status model.
4. Implement target resolution and status-only reconciliation.
5. Implement safe `OnDelete` rollout orchestration.
6. Implement watched Secret and ConfigMap drift handling.
7. Implement policy-driven cert rotation handling.
8. Implement hibernation and restore tracking.
9. Add `envtest`, Helm, and kind coverage.
10. Validate AKS first and document OpenShift-specific adjustments.

## Current Scaffold Status

What is runnable now:

- the standalone Helm chart can render and deploy a minimal real NiFi 2 cluster on kind
- the repo has a single alpha workflow entrypoint: `make kind-alpha-e2e`
- the repo has phase-level fresh-kind alpha targets for rollout, config drift, TLS, and hibernation debugging
- the chart wires Kubernetes leader election and ConfigMap-backed cluster state settings through explicit NiFi configuration rather than hidden controller behavior
- the chart mounts persistent repositories, config, Services, and probes suitable for a kind-focused local workflow
- the repo includes a repeatable health-check flow that separates pod readiness, secured API reachability, and actual cluster convergence
- the optional controller can coordinate managed `OnDelete` rollouts one pod at a time for StatefulSet template drift, revision drift, explicitly watched non-TLS config drift, and TLS drift that policy marks restart-required
- the optional controller now coordinates NiFi disconnect and offload before managed pod deletion or replica reduction
- the optional controller can hibernate a managed cluster by capturing the last running replica count, stepping replicas down highest ordinal first, and restoring back to the recorded size
- the repo includes a minimal in-cluster controller deployment path for local kind verification

What is still intentionally stubbed:

- production-grade TLS automation beyond documented Secret expectations
- production-hardening of chart defaults, auth choices, and storage layouts
- richer restore target memory than `status.hibernation.lastRunningReplicas` plus the current `1` replica fallback

Current alpha status:

- `make kind-alpha-e2e` is green end to end and is the private-alpha gate
- failures dump `NiFiCluster`, `StatefulSet`, pod revision and UID state, controller logs, and relevant events
- CI can upload those diagnostics from `ARTIFACT_DIR` on failure
- the fresh kind workflow preloads `apache/nifi:2.0.0` into the kind node before Helm install so bootstrap does not depend on an in-cluster registry pull

Implementation note for this slice:

- the chart default still keeps NiFi TLS autoreload configurable and off by default for the minimal standalone path
- the managed example enables NiFi TLS autoreload so the local TLS policy flow exercises the intended autoreload-first design

## Local Kind Flow

The exact local flow is documented in [docs/local-kind.md](docs/local-kind.md).

Standalone short version:

1. `make kind-up`
2. `make kind-load-nifi-image`
3. `make kind-secrets`
4. `make helm-install-standalone`
5. `make kind-health`

`make kind-health` is the authoritative local verification flow for this repository. It reports three distinct stages:

- Kubernetes pod readiness
- secured NiFi API reachability on every pod
- NiFi cluster convergence from every pod's local `flow/cluster/summary` view

The script exits successfully only after the convergence signal stays healthy for three consecutive polls. On a fresh 3-node kind install with the current standalone example, one measured run reached:

- all pods `Ready` at about `+116s`
- secured API reachability on all pods at about `+116s`
- full NiFi convergence at about `+134s`
- three consecutive healthy convergence polls at about `+160s`

Treat those numbers as an observed baseline, not a hard SLA.

Managed rollout short version:

1. `make kind-up`
2. `make kind-load-nifi-image`
3. `make kind-secrets`
4. `make install-crd`
5. `make docker-build-controller`
6. `make kind-load-controller`
7. `make deploy-controller`
8. `kubectl -n nifi-system rollout status deployment/nifi2-platform-controller-manager --timeout=5m`
9. `make helm-install-managed`
10. `make apply-managed`
11. `make kind-health`
12. `helm upgrade --install nifi charts/nifi -n nifi -f examples/managed/values.yaml --reuse-values --set-string podAnnotations.rolloutNonce=$(date +%s)`
13. `make kind-config-drift`
14. `make kind-tls-drift`
15. `make kind-hibernate`
16. `make kind-restore`

Private-alpha full path:

1. `make kind-alpha-e2e`

The command provisions a fresh kind cluster, installs the managed chart and controller, runs the health gate, exercises managed rollout, config drift, TLS observe-only, TLS restart-required, hibernation, and restore, then checks controller metrics and events. It exits on the first failing stage and dumps diagnostics.

Phase-level private-alpha paths:

1. `make kind-e2e-rollout`
2. `make kind-e2e-config-drift`
3. `make kind-e2e-tls`
4. `make kind-e2e-hibernate`

Each target provisions a fresh kind cluster and runs only the minimum slice needed for that lifecycle area.

On one clean kind run, the controller advanced the rollout in the expected order: `nifi-2`, then `nifi-1`, then `nifi-0`.

## Proven Workflow Coverage

The current private-alpha package is proven by:

- `go test ./...`
- `helm lint charts/nifi`
- `helm template` for standalone and managed examples
- `make kind-alpha-e2e`

The end-to-end gate covers:

- managed install
- per-pod health gate
- managed revision rollout
- config drift rollout
- TLS observe-only handling
- TLS restart-required rollout
- hibernation
- restore
- controller events and metrics presence

## Compatibility

Tested with the current private-alpha gate:

- NiFi image: `apache/nifi:2.0.0`
- Helm chart example values in [examples/standalone/values.yaml](/home/michael/Work/nifi2-platform/examples/standalone/values.yaml) and [examples/managed/values.yaml](/home/michael/Work/nifi2-platform/examples/managed/values.yaml)
- kind node image: `kindest/node:v1.31.0`
- Kubernetes behavior expected by the gate:
  - `StatefulSet` with `OnDelete` updates in managed mode
  - direct pod DNS reachability
  - PVC retention on scale-down and delete
  - single-node kind control-plane cluster

Not yet covered by the automated gate:

- AKS runtime validation
- OpenShift runtime validation
- NiFi image tags other than `2.0.0`
- multi-node Kubernetes clusters outside kind
- production ingress and cert-manager automation paths

## Private Alpha Release

What is proven:

- a new evaluator can install the standalone chart and get a healthy NiFi 2 cluster
- a new evaluator can install managed mode and exercise the current lifecycle paths
- the repo CI and local gate are aligned on the same end-to-end workflow

Support and status expectations:

- this is private-alpha software
- expect fast iteration and breaking changes between alpha tags
- support expectations are best-effort engineering collaboration, not a production SLA
- bugs should be reported with the failing command, `NiFiCluster` output, controller logs, and any `ARTIFACT_DIR` bundle

## Known Limitations

- This is still private-alpha quality, not production-hardening guidance.
- cert-manager remains an external contract rather than a fully automated chart path.
- restore still falls back to `1` replica only when neither `baselineReplicas` nor `lastRunningReplicas` is present.
- OpenShift remains a secondary compatibility target behind AKS-first behavior and kind validation.
- the repo directory name and Go module name are not yet fully aligned for a public release decision.
- `make kind-alpha-e2e` currently assumes the alpha chart image stays aligned with `make kind-load-nifi-image`; update both together if the NiFi image tag changes.

## Intentionally Out Of Scope

- new CRDs beyond `NiFiCluster`
- NiFi 1.x support
- flow, user, policy, or registry management APIs
- backup and restore orchestration
- autoscaling
- broader lifecycle scope than the existing managed restart, TLS, and hibernation behavior

## Release Prep

Version and tag guidance:

- use explicit pre-release tags such as `v0.1.0-alpha.1`
- tag only from commits that pass `make kind-alpha-e2e`
- keep chart and controller version bumps aligned
- call out the tested NiFi image and kind/Kubernetes assumptions in the release notes

Private repo checklist:

- confirm repo visibility before publishing any tag
- confirm controller image name and registry path before adding release automation
- keep CI artifact upload enabled for failed alpha runs
- confirm the GitHub runner image still has Docker support for kind-based jobs

Module and repo naming TODO:

- if the final repository path changes, update [go.mod](/home/michael/Work/nifi2-platform/go.mod) and imports before the first non-alpha tag

Managed watched-drift behavior:

- every `spec.restartTriggers.configMaps[]` entry contributes to config drift
- a watched Secret contributes to certificate drift only when it is the same Secret mounted as the target StatefulSet TLS volume
- every other watched Secret contributes to config drift
- config drift reuses the same managed `OnDelete` rollout path as StatefulSet revision drift
- stable TLS content drift follows `spec.restartPolicy.tlsDrift`
- material TLS ref, mount path, or password-key changes are treated as restart-required
- the current autoreload observation window is `30s`

Managed hibernation behavior:

- managed restart and hibernation now persist `status.nodeOperation` while NiFi prepares the target node for removal
- the controller asks NiFi to disconnect the target node, waits for `DISCONNECTED`, then asks NiFi to offload it and waits for `OFFLOADED`
- `spec.desiredState=Hibernated` captures `status.hibernation.lastRunningReplicas` and then steps the target StatefulSet down one replica at a time, highest ordinal first
- PVCs are preserved because the controller only changes `StatefulSet.spec.replicas`
- `spec.desiredState=Running` restores the prior size from `status.hibernation.lastRunningReplicas`
- if `status.hibernation.lastRunningReplicas` is absent, the controller falls back to `1` replica
- restore does not report success until pod readiness, secured API reachability, and stable cluster convergence return

## Operator UX

Most useful status view:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.lastOperation.phase}{"\n"}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
```

Example output shape:

```text
Succeeded
Revision "nifi-66f744c9b6" is fully rolled out and healthy
TargetResolved: TargetFound True
Available: RolloutHealthy True
Progressing: NoDrift False
Degraded: AsExpected False
Hibernated: Running False
```

Most useful debug commands:

- `kubectl -n nifi get nificluster nifi -o yaml`
- `kubectl -n nifi describe nificluster nifi`
- `kubectl -n nifi get sts nifi -o custom-columns=NAME:.metadata.name,SPEC:.spec.replicas,READY:.status.readyReplicas,CURRENT:.status.currentRevision,UPDATE:.status.updateRevision`
- `kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,READY:.status.containerStatuses[0].ready,REV:.metadata.labels.controller-revision-hash,UID:.metadata.uid,DEL:.metadata.deletionTimestamp`
- `kubectl -n nifi-system logs deployment/nifi2-platform-controller-manager --tail=200`
- `kubectl -n nifi get events --sort-by=.lastTimestamp | tail -n 50`
- `make kind-health`

Local drift verification commands:

```bash
make kind-config-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedConfigHash}{"\n"}{.status.rollout.trigger}{"\n"}{.status.rollout.targetConfigHash}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready \
  --watch
make kind-health
```

```bash
make kind-tls-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.observedCertificateHash}{"\n"}{.status.observedTLSConfigurationHash}{"\n"}{.status.tls.observationStartedAt}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready
```

```bash
make kind-tls-config-drift
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.rollout.targetCertificateHash}{"\n"}{.status.rollout.targetTLSConfigurationHash}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi get pods \
  -o custom-columns=NAME:.metadata.name,DEL:.metadata.deletionTimestamp,READY:.status.containerStatuses[0].ready \
  --watch
make kind-health
```

Expected results:

- `make kind-config-drift` should trigger a one-pod-at-a-time managed rollout and then settle back to a healthy cluster
- `make kind-tls-drift` should enter a TLS autoreload observation window and, if health stays good, reconcile without pod deletion
- `make kind-tls-config-drift` should trigger a one-pod-at-a-time managed TLS rollout because the TLS mount path changed
- managed rollout should show `PreparingNodeForRestart` before the controller deletes the next pod
- `make kind-hibernate` should step the managed StatefulSet toward `0` one ordinal at a time while preserving PVCs and setting `Hibernated=True` only at completion
- `make kind-restore` should restore the prior replica count from status and wait for the same per-pod health gate before reporting success

## Standalone Health Gate

The future controller should reuse the same health gate that the standalone verification flow uses today.

Authoritative signal:

- the target `StatefulSet` has the expected number of `Ready` pods
- each pod can mint a local token against its own HTTPS endpoint
- each pod's own `https://<pod>.<headless-service>.<namespace>.svc.cluster.local:8443/nifi-api/flow/cluster/summary` reports:
  - `clustered=true`
  - `connectedToCluster=true`
  - `connectedNodeCount == expected replicas`
  - `totalNodeCount == expected replicas`
- the cluster summary condition holds across three consecutive polls

Important constraints:

- do not use the ClusterIP Service as the authoritative convergence check because it hides which pod view you are reading
- do not assume a token minted on one pod is reusable on another pod
- do not treat `Ready=True` alone as cluster convergence

Fallback diagnostic signal:

- if all pods are `Ready` and each pod's secured API is reachable but the cluster summary is still lagging, report that as `startup in progress`
- future managed rollout logic should requeue on that condition rather than advancing a restart or hibernation step

The kind helper stores `ca.crt` in the TLS Secret and also creates a PKCS12 truststore, using local `keytool` when available or a disposable `apache/nifi:2.0.0` container when it is not.

Useful local commands:

- `make fmt`
- `make test`
- `make helm-lint`
- `make kind-up`
- `make kind-load-nifi-image`
- `make kind-secrets`
- `make install-crd`
- `make docker-build-controller`
- `make kind-load-controller`
- `make deploy-controller`
- `make helm-install-standalone`
- `make kind-health`
- `make kind-config-drift`
- `make kind-tls-drift`
- `make kind-tls-config-drift`
- `make kind-hibernate`
- `make kind-restore`
- `make helm-install-managed`
- `make apply-managed`
- `make run`

## References

- Apache NiFi Administration Guide: https://nifi.apache.org/documentation/nifi-latest/html/administration-guide.html
- Apache NiFi REST API: https://nifi.apache.org/nifi-docs/rest-api.html
- NiFiKop repository for lessons, not compatibility: https://github.com/konpyutaika/nifikop
