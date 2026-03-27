# Flow-Change Audit

Status: Available in bounded form

This page describes the customer-facing flow-change audit capability in NiFi-Fabric.

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

The supported model is hybrid.

NiFi-native local audit remains the base layer:

- Flow Configuration History
- persisted `nifi.database.directory`
- automatic flow archive retention
- request logs as secondary evidence

NiFi-Fabric adds one bounded export path:

- a custom `FlowActionReporter`
- packaged and configured by the chart

This stays separate from:

- metrics
- provenance
- Site-to-Site observability sender paths

This feature is intentionally optional.

The default product posture is:

- local NiFi-native audit on
- external flow-action export off

Customers who want external export opt into the bounded reporter path explicitly.

## Why This Is Not Metrics

Metrics answer questions like:

- how healthy is the cluster
- what is the queue depth
- how much throughput are we seeing

Design-time audit answers a different question:

- who changed the flow definition and what object changed

For that reason, the API shape is a sibling capability:

- `observability.audit.flowActions.*`

and not:

- `observability.metrics.*`

## Capability Shape

The current values shape is:

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

The chart supports one bounded advanced export path:

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

## Customer Connectivity And Registry Model

The `export.type=log` reporter path does not require public registry access as a product assumption.

The intended customer model is:

- connected environments can use the published reporter image directly
- restricted environments can mirror that image into an internal registry and point the chart at the mirrored repository and tag
- fully air-gapped environments can build the image from the published source or NAR artifact and host it internally

The public GHCR image is a convenience, not a dependency.

Customers should treat these settings as normal operator-managed image coordinates:

- `observability.audit.flowActions.export.log.installation.image.repository`
- `observability.audit.flowActions.export.log.installation.image.tag`
- `imagePullSecrets` when the selected registry requires authentication

Example connected-cluster shape:

```yaml
observability:
  audit:
    flowActions:
      enabled: true
      export:
        type: log
        log:
          installation:
            image:
              repository: ghcr.io/example-org/nifi-fabric-flow-action-audit-reporter
              tag: 0.1.0
```

Example private-registry shape:

```yaml
imagePullSecrets:
- name: internal-registry-creds

observability:
  audit:
    flowActions:
      enabled: true
      export:
        type: log
        log:
          installation:
            image:
              repository: registry.example.com/platform/nifi-fabric-flow-action-audit-reporter
              tag: 0.1.0
```

For customers who do not want this path yet, keep:

```yaml
observability:
  audit:
    flowActions:
      enabled: false
```

or keep export disabled while retaining the local audit layer:

```yaml
observability:
  audit:
    flowActions:
      enabled: true
      export:
        type: disabled
```

The intent behind each area is:

- `enabled`: turns on the bounded audit-export feature
- `local.history.enabled`: keeps the NiFi-native history path explicit in the product surface
- `local.archive.*`: makes the archive durable and operator-visible instead of relying on the NiFi default under `./conf/archive`
- `local.requestLog.*`: keeps request logging available as secondary evidence
- `export.type`: reserved for the later external export slice
- `content.*`: controls bounded enrichment and redaction behavior

## What Is Included Today

Today this feature includes:

- explicit `observability.audit.flowActions.*` values
- NiFi property wiring for `nifi.database.directory`
- durable flow archive settings
- optional request-log format wiring
- Helm validation that keeps this slice bounded and supportable
- bounded `export.type=log` reporter wiring for advanced installs that provide a reporter image
- published reporter artifact and image build or release plumbing

This feature does not yet include:

- non-log sinks such as HTTP or Kafka
- anything beyond the bounded log-only reporter path

## Production Rollout

Use a small staged rollout for this feature.

1. Start with local audit only.
   Keep `observability.audit.flowActions.enabled=true` and `export.type=disabled` so the durable local support layer is active before adding the reporter path.
2. Validate reporter image reachability.
   Pick one explicit image source:
   - published image for connected clusters
   - mirrored internal image for restricted clusters
   - internally built image for air-gapped clusters
3. Enable `export.type=log` only after the init-container pull path and NAR installation path are confirmed in the target environment.

Useful examples:

- [platform-managed-audit-flow-actions-values.yaml](../../examples/platform-managed-audit-flow-actions-values.yaml)
  - generic advanced overlay used in repo validation
- [platform-managed-audit-flow-actions-ghcr-values.yaml](../../examples/platform-managed-audit-flow-actions-ghcr-values.yaml)
  - connected-cluster published-image example
- [platform-managed-audit-flow-actions-private-registry-values.yaml](../../examples/platform-managed-audit-flow-actions-private-registry-values.yaml)
  - restricted-cluster mirrored-image example

Representative install commands:

```bash
helm upgrade --install nifi charts/nifi-platform \
  -n nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-audit-flow-actions-ghcr-values.yaml
```

```bash
helm upgrade --install nifi charts/nifi-platform \
  -n nifi \
  --create-namespace \
  -f examples/platform-managed-values.yaml \
  -f examples/platform-managed-audit-flow-actions-private-registry-values.yaml
```

For production, pin the reporter image tag explicitly and avoid floating tags except for short-lived validation environments.

Operator-facing tag selection and first-response checks are in:

- [Operations Runbooks](../operations/runbooks.md)
  - see `Flow-Action Audit Reporter Image Selection`

## Event Shape

The exported event is intentionally minimal and stable:

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

The current reporter implementation emits a structure like:

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

The export path is fail open.

If external audit export is unhealthy:

- NiFi flow changes must still succeed
- the local NiFi history remains the primary fallback
- the reporter should log export failures clearly

The product does not promise exactly-once delivery.

## Cluster and Storage Notes

Cluster identity should be included in exported events so downstream systems can separate:

- logical cluster identity
- emitting node identity

The chart already moves the flow archive to durable storage rather than relying on NiFi's default `./conf/archive` path under `conf`.

## Out Of Scope

This feature does not aim to become:

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
