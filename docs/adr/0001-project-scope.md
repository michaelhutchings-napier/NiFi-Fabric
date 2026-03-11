# ADR 0001: Project Scope

- Status: Accepted
- Date: 2026-03-08

## Context

The project starts from an empty repository and needs a clear scope before any chart or controller code exists. NiFi 2.x changes the operator design space because Kubernetes-native coordination and shared state support already exist upstream.

Without an explicit scope decision, the project could expand toward a broad NiFi operator with hard-to-explain APIs and weak GitOps ergonomics.

## Decision

The project will:

- support Apache NiFi 2.x only
- target AKS first and OpenShift-friendly operation second
- provide a top-level product Helm chart as the primary installation path
- keep a standalone Helm app chart available for lower-level installs
- provide an optional thin controller for lifecycle and safety orchestration
- use one namespaced operational CRD in MVP
- stay GitOps-first and TLS-first

The project will not:

- support NiFi 1.x
- pursue NiFiKop compatibility or feature parity
- add advanced dataflow, user, registry, or backup management in MVP
- introduce additional CRDs unless justified in `docs/api.md` and a new ADR

## Consequences

- The design remains understandable by one engineer reading it for the first time.
- Helm values remain the main deployment surface, with one product-facing chart and one reusable app chart.
- The controller must justify every responsibility by showing why Helm or NiFi native behavior is insufficient.
- Future scope expansion must pass a high bar and stay consistent with the thin-platform intent.
