# ADR 0008: Flow-Action Audit Export

- Status: Proposed
- Date: 2026-03-26

## Context

NiFi-Fabric already has a clear observability split for metrics, status, and provenance-oriented export paths.

It does not yet have a productized answer for design-time audit of user-made flow changes such as:

- creating, updating, moving, connecting, or deleting processors
- editing process groups
- changing controller services
- changing Parameter Context values or attachments
- changing connections and version-control attachments

NiFi already provides several related native capabilities:

- Flow Configuration History stored in the internal database repository
- automatic flow archive creation
- HTTP request logging
- the `FlowActionReporter` extension point for exporting flow actions

The platform needs a production-minded answer for "who changed what, when" without:

- overloading `observability.metrics`
- treating provenance as configuration audit
- adding a new CRD
- turning the product into a generic SIEM or audit platform

## Decision

The product direction is a hybrid audit model.

NiFi-native local audit remains the base layer:

- persisted Flow Configuration History
- automatic flow archive retention
- request logs as secondary evidence

NiFi-Fabric adds one bounded external export path:

- a custom `FlowActionReporter`
- surfaced as a sibling capability under a future `observability.audit.flowActions` values tree

The audit capability is intentionally separate from:

- `observability.metrics.*`
- `observability.siteToSiteStatus.*`
- `observability.siteToSiteProvenance.*`

The controller does not own this feature.

Helm and the workload chart own the future wiring for:

- reporter configuration
- archive directory configuration
- archive retention settings
- optional export sink settings

The first supported export mode should be a structured JSON log sink.

That keeps the product bounded:

- NiFi-Fabric produces structured audit events
- the environment's normal logging platform handles shipping, retention, indexing, and alerting

## Design Boundaries

The future audit feature should:

- stay NiFi-native first
- fail open when export sinks are unhealthy
- never block user flow changes because audit export is unavailable
- redact property values by default
- keep cluster and node identity visible in the exported event

The future audit feature should not:

- become a generic replay or backfill system
- promise exactly-once delivery
- export sensitive values by default
- add direct controller reconciliation loops
- introduce a new API object or control plane

## Operational Implications

The default NiFi flow archive directory is under `./conf/archive`.

In the current app chart, `conf` is mounted from `emptyDir`, so a future implementation must not rely on that default path for durable support workflows.

The future implementation should point the archive at a persisted location, most likely under the database repository or another PVC-backed path.

Request logs remain supplemental:

- useful for tracing attempted or denied requests
- not the primary object-level change ledger

The local audit store is important for support and rollback investigation, but external retention and search remain operator-owned.

## Consequences

- The product keeps a simple, explainable audit model.
- Metrics, provenance, and design-time audit stay separate.
- Helm-first ownership remains intact.
- The recommended export path stays compatible with GitOps and existing cluster log pipelines.
