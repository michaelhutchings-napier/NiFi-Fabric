# nifi2-platform

`nifi2-platform` is a thin, modern platform layer for running Apache NiFi 2.x on Kubernetes.

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
- the chart wires Kubernetes leader election and ConfigMap-backed cluster state settings through explicit NiFi configuration rather than hidden controller behavior
- the chart mounts persistent repositories, config, Services, and probes suitable for a kind-focused local workflow
- the repo includes a repeatable health-check flow that separates pod readiness, secured API reachability, and actual cluster convergence
- the controller remains status-only and optional

What is still intentionally stubbed:

- advanced controller rollout, cert drift, and hibernation orchestration
- production-grade TLS automation beyond documented Secret expectations
- production-hardening of chart defaults, auth choices, and storage layouts

Implementation note for this slice:

- standalone mode keeps NiFi TLS autoreload configurable but disabled by default so the minimal local cluster starts cleanly; the cert-rotation/controller slice will revisit the full autoreload-first policy

## Local Kind Flow

The exact local flow is documented in [docs/local-kind.md](docs/local-kind.md).

The short version is:

1. `make kind-up`
2. `make kind-secrets`
3. `make helm-install-standalone`
4. `make kind-health`

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
- `make kind-secrets`
- `make helm-install-standalone`
- `make kind-health`
- `make helm-install-managed`
- `make run`

## References

- Apache NiFi Administration Guide: https://nifi.apache.org/documentation/nifi-latest/html/administration-guide.html
- Apache NiFi REST API: https://nifi.apache.org/nifi-docs/rest-api.html
- NiFiKop repository for lessons, not compatibility: https://github.com/konpyutaika/nifikop
