# Observability Audit Design

Status: Partially implemented

This page describes the product design and current implementation direction for design-time audit support in NiFi-Fabric.

It is intentionally separate from the current metrics and provenance features.

## Problem Statement

NiFi-Fabric needs a supportable production answer for user-made flow changes such as:

- creating, editing, moving, connecting, or deleting processors and process groups
- changing controller services
- changing Parameter Contexts
- changing connections and version-control attachments

The product answer needs to tell operators:

- who made the change
- what changed
- when it changed
- how to export that evidence externally

## Product Position

The planned model is hybrid.

NiFi-native local audit remains the base layer:

- Flow Configuration History
- persisted `nifi.database.directory`
- automatic flow archive retention
- request logs as secondary evidence

NiFi-Fabric plans one bounded export path:

- a custom `FlowActionReporter`
- packaged and configured by the chart

This stays separate from:

- metrics
- provenance
- Site-to-Site observability sender paths

## Why This Is Not Metrics

Metrics answer questions like:

- how healthy is the cluster
- what is the queue depth
- how much throughput are we seeing

Design-time audit answers a different question:

- who changed the flow definition and what object changed

For that reason, the planned API shape is a future sibling capability such as:

- `observability.audit.flowActions.*`

and not:

- `observability.metrics.*`

## Planned Capability Shape

The current values shape for the first implementation slice is:

```yaml
observability:
  audit:
    flowActions:
      enabled: false
      local:
        history:
          enabled: true
        archive:
          enabled: true
          directory: /opt/nifi/nifi-current/database_repository/flow-audit-archive
          retention:
            maxAge: 30 days
            maxStorage: 2 GB
            maxCount: 1000
        requestLog:
          enabled: true
          format: '%{client}a - %u %t "%r" %s %O "%{Referer}i" "%{User-Agent}i"'
      export:
        type: disabled
        log:
          installation:
            image:
              repository: ""
              tag: ""
              pullPolicy: IfNotPresent
              narPath: /opt/nifi-fabric-audit/nifi-flow-action-audit-reporter.nar
      content:
        includeRequestDetails: true
        includeProcessGroupPath: true
        propertyValues:
          mode: redacted
          allowlistedProperties: []
```

The local-layer values above are implemented in the chart.

The chart now also supports one bounded advanced export path:

- `export.type=log`

That path:

- wires `nifi.flow.action.reporter.implementation`
- configures a dedicated additional NAR directory for the reporter
- installs the reporter NAR from an operator-supplied image through a chart-managed init container

Example managed overlay:

- `examples/platform-managed-audit-flow-actions-values.yaml`

Focused kind proof overlay:

- `examples/platform-managed-audit-flow-actions-kind-values.yaml`
- this keeps the local proof on one NiFi node and grants the bootstrap admin only the bounded mutable-flow capability needed to create one root-child process group

Helper build command for the example image:

- `make build-flow-action-audit-reporter-image`

Focused kind proof command:

- `make kind-flow-action-audit-fast-e2e`

Minimum supported NiFi version for `export.type=log`:

- `2.4.0`

The intent behind each area is:

- `enabled`: turns on the bounded audit-export feature
- `local.history.enabled`: keeps the NiFi-native history path explicit in the product surface
- `local.archive.*`: makes the archive durable and operator-visible instead of relying on the NiFi default under `./conf/archive`
- `local.requestLog.*`: keeps request logging available as secondary evidence
- `export.type`: reserved for the later external export slice
- `content.*`: controls bounded enrichment and redaction behavior

## Current Implementation Slice

The current implemented slice is the durable local layer:

- explicit `observability.audit.flowActions.*` values
- NiFi property wiring for `nifi.database.directory`
- durable flow archive settings
- optional request-log format wiring
- Helm validation that keeps this slice bounded and supportable
- bounded `export.type=log` reporter wiring for advanced installs that provide a reporter image

The repository also now contains the reporter source scaffold:

- `extensions/nifi-flow-action-audit-reporter-bundle/`
- a fixed JSON logger reporter implementation
- standalone Maven and NAR packaging files
- a helper build script under `hack/build-flow-action-audit-reporter-nar.sh`
- a focused kind proof path under `hack/kind-flow-action-audit-e2e.sh`
- CI and release plumbing that builds the reporter NAR and image predictably

The current implementation does not yet include:

- non-log sinks such as HTTP or Kafka
- anything beyond the bounded log-only reporter path

## Reporter Artifact Build And Release

The reporter is now treated as a first-class repository artifact rather than an ad hoc local build:

- `ci` builds the reporter NAR and the minimal reporter image on ordinary validation runs
- `.github/workflows/flow-action-audit-reporter-release.yaml` uploads the built NAR as a workflow artifact on pull requests
- pushes to `main` or `master` publish a GHCR image at `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:edge` plus `:sha-<commit>`
- a tag named `flow-action-audit-reporter-vX.Y.Z` publishes the image tag `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:X.Y.Z` and a matching GitHub release asset containing the built NAR

The focused runtime proof is now also separated into its own CI lane:

- `.github/workflows/flow-action-audit-kind-e2e.yaml` runs `make kind-flow-action-audit-fast-e2e`
- it is triggered only when the audit path or its shared runtime/build inputs change, so it stays targeted instead of inflating the generic kind matrix

Useful helper commands:

- `make print-flow-action-audit-reporter-version`
- `make build-flow-action-audit-reporter-dist`

## Implementation Shape

The future implementation should follow the existing chart and docs conventions:

- values under `observability.*`
- Helm validation in `_helpers.tpl`
- workload configuration rendered through the main chart `ConfigMap`
- render tests in `test/unit/*_render_test.go`
- one focused example overlay per supported product path

The recommended internal reporter class should not be customer-configurable in the MVP.

The chart owns the wiring for a single product-managed reporter implementation and exposes only the bounded operator settings above.

## File-by-File Implementation Plan

### Docs

- `docs/manage/observability-audit.md`
  - keep the product behavior, event model, and failure model here
- `docs/reference/app-chart-values.md`
  - document the implemented local-layer values and their current support boundary
- `docs/reference/nifi-platform-values.md`
  - document the nested `nifi.observability.audit.*` reference for the implemented local-layer fields
- `docs/features.md`
  - add one short bullet when external export is no longer design-only
- `docs/operations/runbooks.md`
  - add one focused runbook section for audit-export failure and local fallback

### Chart values and validation

- `charts/nifi/values.yaml`
  - add the new `observability.audit.flowActions.*` block
- `charts/nifi-platform/values.yaml`
  - document the nested `nifi.observability.audit.*` pass-through in the managed chart comments
- `charts/nifi/templates/_helpers.tpl`
  - add validation for:
  - supported `export.type` values
  - required log settings
  - redaction mode values
  - archive retention contradictions or empty required fields

### Workload configuration

- `charts/nifi/templates/configmap.yaml`
  - render NiFi property overrides such as:
  - `nifi.flow.configuration.archive.enabled`
  - `nifi.flow.configuration.archive.dir`
  - retention properties for max time, storage, and count
  - `nifi.web.request.log.format` when the feature explicitly manages request-log format
  - `nifi.nar.library.directory.flow.action.audit`
  - `nifi.flow.action.reporter.implementation`
- `charts/nifi/templates/statefulset.yaml`
  - mount the advanced reporter extension directory
  - add the reporter-image init container for the bounded `log` export path
  - rely on the main config checksum annotation for rollout on config changes

### Runtime assets

- `charts/nifi/templates/flow-action-audit-configmap.yaml`
  - optional, only if the reporter needs a mounted JSON or YAML config file beyond simple `nifi.properties` overrides
- `extensions/nifi-flow-action-audit-reporter-bundle/`
  - reporter module
  - one implementation class
  - JSON event mapping
  - redaction and allowlist logic
  - fail-open logging behavior

### Tests

- `test/unit/audit_render_test.go`
  - new render coverage for enabled and disabled states
  - validation errors
  - archive path rendering
  - logger and reporter wiring
- `hack/kind-flow-action-audit-e2e.sh`
  - kind runtime proof that:
  - a flow edit emits an audit event
  - archive files land on durable storage
  - restart preserves local audit state
- `examples/platform-managed-audit-flow-actions-values.yaml`
  - managed-chart example for the bounded log-only export path
  - one focused example overlay for the managed path

## Delivery Phases

The recommended delivery plan is:

1. Ship the durable local layer first.

That means:

- explicit archive configuration
- persisted archive path
- request-log formatting guidance
- no external export yet

2. Ship the bounded JSON log reporter.

That means:

- one supported sink type
- one reporter implementation
- one chart-managed NAR image install path
- fail-open behavior
- redaction by default

3. Add advanced sinks only if customers justify them.

That means considering:

- `http`
- `kafka`

and only after the JSON log path is stable and supportable.

## MVP Acceptance Criteria

The MVP should be considered complete when all of the following are true:

- enabling the feature renders the expected workload configuration
- disabling the feature removes reporter-specific wiring cleanly
- the archive path is durable and no longer relies on `./conf/archive`
- a processor create or configure action emits one structured audit event
- the event contains user, timestamp, operation, and component identity fields
- property values are redacted by default
- reporter sink failure does not block the user action
- docs explain the support boundary and fallback to local NiFi history

## Test Strategy

The recommended test strategy mirrors the current product style.

Render-test coverage should verify:

- values validation
- generated NiFi property overrides
- optional auxiliary ConfigMap rendering
- example overlay rendering

Runtime coverage should verify:

- create, update, move, and delete-style flow actions emit events
- request details are included when available
- archive files survive restart because they are on durable storage
- reporter failure is visible in logs and does not block the edit path

Security coverage should verify:

- sensitive values are redacted by default
- allowlisted values only appear when explicitly enabled
- the feature does not require a new broad management credential model

## Recommended MVP

The recommended MVP is intentionally small:

- one packaged reporter
- one supported sink type: structured JSON log
- local history and archive retained
- request logs kept as secondary evidence
- property values redacted by default

That model lets the environment's normal log shipping stack handle:

- external delivery
- retention
- indexing
- alerting

## Event Model

The planned exported event should stay minimal and stable:

- `timestamp`
- `actionId`
- `user.identity`
- `action.operation`
- `component.id`
- `component.type`
- `component.name` when NiFi provides `ACTION_DETAILS_NAME`
- `processGroup.id`
- `processGroup.previousId` when an action moves across groups
- `request.remoteAddress`
- `request.forwardedFor`
- `request.userAgent`
- `change.source.id`
- `change.source.type`
- `change.destination.id`
- `change.destination.type`
- `change.relationship`
- raw `attributes` for support correlation

Property values should be redacted by default.

Any future non-redacted export should be explicit and allowlist-based.

The current reporter implementation now emits the following cleaner structure on top of the raw NiFi attributes:

```json
{
  "schemaVersion": "v1",
  "eventType": "nifi.flowAction",
  "timestamp": "2026-03-27T16:00:00Z",
  "actionId": "1234",
  "user": {
    "identity": "admin"
  },
  "action": {
    "operation": "Add"
  },
  "component": {
    "id": "component-1",
    "type": "ProcessGroup",
    "name": "Ingest"
  },
  "processGroup": {
    "id": "root-group"
  },
  "change": {
    "source": {
      "id": "source-1",
      "type": "PROCESSOR"
    },
    "destination": {
      "id": "destination-1",
      "type": "PROCESS_GROUP"
    },
    "relationship": "success"
  },
  "request": {
    "remoteAddress": "10.0.0.15"
  }
}
```

This is still intentionally not a full before/after diff engine.

## Failure Model

The planned export path is fail open.

If external audit export is unhealthy:

- NiFi flow changes must still succeed
- the local NiFi history remains the primary fallback
- the reporter should log export failures clearly

The product should not promise exactly-once delivery in the MVP.

## Cluster and Storage Notes

Cluster identity should be included in exported events so downstream systems can separate:

- logical cluster identity
- emitting node identity

The future implementation must also move the flow archive to durable storage.

Today the chart mounts `conf` from `emptyDir`, so the default NiFi archive location under `./conf/archive` is not durable enough for this planned feature.

## Out of Scope

The planned feature does not aim to become:

- a generic SIEM
- a long-term immutable evidence store
- a replay engine
- a policy-denied audit system
- a generic flow diff and snapshot engine

## Related Docs

- [Observability and Metrics](observability-metrics.md)
- [Architecture Summary](../architecture.md)
- [Roadmap](../roadmap.md)
- [ADR 0008: Flow-Action Audit Export](../adr/0008-flow-action-audit-export.md)
