# Architecture Summary

NiFi-Fabric is a NiFi 2-first Kubernetes platform with a small, explicit architecture.

It is built around a standard Helm install path, a thin controller for lifecycle and safety, and clear ownership boundaries between Helm, Kubernetes, NiFi, and the operator.

## Product Shape

NiFi-Fabric has two chart surfaces:

- `charts/nifi-platform`: the standard customer-facing install path
- `charts/nifi`: the reusable standalone NiFi app chart

For most teams, `charts/nifi-platform` is the right starting point.

## Core Components

The standard managed architecture includes:

- the `charts/nifi-platform` Helm chart
- the `charts/nifi` workload chart
- a managed `NiFiCluster` resource
- the NiFi-Fabric controller
- the NiFi `StatefulSet`

This keeps the product model simple:

- Helm renders and owns standard Kubernetes resources
- NiFi keeps its native runtime behavior
- the controller handles the lifecycle work Helm cannot do safely on its own

## Who Owns What

### Helm owns

- the install surface
- standard Kubernetes resources such as the `StatefulSet`, Services, PVCs, ingress, and Route resources
- cert-manager `Certificate` resources when that path is enabled
- optional trust-manager resources when that path is enabled
- chart-managed bootstrap inputs for the standard quickstart path
- metrics Services and `ServiceMonitor` resources
- configuration features such as Parameter Context definitions, Flow Registry Client catalogs, and versioned-flow import declarations

### NiFi owns

- clustering behavior
- authentication provider behavior
- the NiFi API and runtime signals
- persisted NiFi runtime state on storage

### The controller owns

- rollout sequencing
- Secret and TLS input readiness status for managed running clusters, including the standard `nifi-auth`, `nifi-tls`, and `nifi-tls-params` contracts when referenced
- TLS-aware restart decisions
- hibernation and restore sequencing
- controller-owned autoscaling execution
- lifecycle safety checks, status, and events

## Managed Lifecycle Model

NiFi-Fabric uses one lifecycle control plane in managed mode.

The controller is the only component that executes destructive lifecycle actions such as:

- rollout pod deletion
- hibernation and restore transitions
- managed autoscaling execution

This is why direct autoscaler ownership of the NiFi `StatefulSet` is not the product model.

Autoscaling stays intentionally conservative:

- the controller owns actual scale execution
- scale-up and scale-down remain conservative and explainable
- scale-down runs as safe one-node steps
- optional KEDA integration can express external scale intent, but the controller still decides whether and when a safe action happens

For autoscaling detail, see [Autoscaling](manage/autoscaling.md) and [KEDA](keda.md).

## Security, TLS, and Metrics

The standard production path is:

- Helm-first
- cert-manager-first
- secure by default

The standard managed install uses cert-manager for workload TLS and can bootstrap the auth or parameter Secrets it needs for the quickstart path. When you later move to the explicit cert-manager path and keep the same Secret names, the chart preserves those previously generated quickstart Secrets so the handoff stays stable.

The controller does not create or mutate those Secrets. It only reports whether the referenced Secret inputs are present and structurally usable before managed running-state orchestration continues. For TLS drift, it also reports the current TLS decision state in `status.tls`, including whether the controller is idle, observing NiFi autoreload, or has determined that a controlled restart is required.

For metrics, the primary path is direct secured scraping of the NiFi 2 Prometheus endpoint through the native API path. An optional exporter path is also available when a dedicated `/metrics` endpoint is preferred.

For more detail, see:

- [TLS and cert-manager](manage/tls-and-cert-manager.md)
- [Authentication](manage/authentication.md)
- [Observability and Metrics](manage/observability-metrics.md)

## Configuration Features

NiFi-Fabric supports a small set of runtime-managed configuration features:

- Flow Registry Client catalogs
- Parameter Context management
- versioned-flow import
- optional typed Site-to-Site sender-side observability features

These features are intentionally narrower than a broad NiFi object-management operator. They are designed to solve common product use cases without introducing a large CRD or control-plane surface.

For feature detail, see:

- [Features](features.md)
- [Flow Registry Clients](manage/flow-registry-clients.md)
- [Parameter Contexts](manage/parameters.md)
- [Flows](manage/flows.md)

## Standard and Advanced Paths

The standard customer path is:

- install cert-manager
- create or choose an `Issuer` or `ClusterIssuer`
- install `charts/nifi-platform`

Advanced paths remain available for teams that want:

- explicit Secret ownership
- external TLS ownership
- OIDC or LDAP
- standalone app-chart installation
- generated manifest workflows
- optional external-style authz generation for OIDC group-to-bundle overlays
- separate OIDC tracks for dev bootstrap convenience and customer-owned production setup

For install guidance, see:

- [Install with Helm](install/helm.md)
- [Advanced Install Paths](install/advanced.md)

## Backup and Disaster Recovery

NiFi-Fabric separates:

- control-plane recovery
- data-plane recovery

Control-plane recovery means restoring the declarative Helm and `NiFiCluster` intent.

Data-plane recovery means restoring NiFi repositories and persisted runtime state from durable storage or snapshots.

NiFi-Fabric helps with the declarative install and lifecycle model. Storage protection, snapshots, replay strategy, and recovery objectives remain operator-owned.

For operational guidance, see:

- [Disaster Recovery](dr.md)
- [Operations and Troubleshooting](operations.md)

## Design Principles

NiFi-Fabric is intentionally:

- NiFi 2.x only
- Helm-first
- thin-controller
- GitOps-friendly
- smaller than a broad NiFi operator

The goal is a production-ready platform that stays understandable, supportable, and easy to explain.
