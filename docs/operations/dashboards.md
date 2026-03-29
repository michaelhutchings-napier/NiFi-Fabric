# Operations Dashboards

NiFi-Fabric now includes two small starter dashboards:

- [Grafana starter dashboard JSON](../../ops/grafana/nifi-fabric-starter-dashboard.json)
- [Grafana runtime-health dashboard JSON](../../ops/grafana/nifi-fabric-runtime-health-dashboard.json)

What the operations dashboard covers:

- controller lifecycle transition rate
- rollout start, completion, and failure counts
- TLS observe-only versus restart-required actions
- hibernation and restore operation counts
- node-preparation retries and timeouts
- autoscaling execution transitions
- autoscaling recommended replicas
- autoscaling queue-pressure and CPU sample signals collected from NiFi

Operations dashboard panel inventory:

- `Lifecycle Transitions`
- `Managed Rollouts`
- `TLS Drift Actions`
- `Hibernation and Restore`
- `Node Preparation Outcomes`
- `Autoscaling Execution Transitions`
- `Recommended Replicas`
- `Queue Pressure Samples`
- `CPU Signal Samples`

What the runtime-health dashboard covers:

- starter native API scrape availability
- controller-collected queue-pressure samples
- exporter source health when exporter mode is enabled
- exporter flow-status queue depth, component-state, thread-count, flow-sync, and remote-port panels when exporter mode is enabled

Runtime-health dashboard panel inventory:

- `Native API Scrape Targets Up`
- `Exporter Source Health`
- `Queue Pressure Samples`
- `Exporter Queue Depth`
- `Component States`
- `Flow Sync States`
- `Thread Counts`
- `Remote Port States`

Why the dashboards stay small:

- it focuses on signals the product already exposes
- they do not assume a large dashboard pack or opinionated folder structure
- it avoids overclaiming environment support beyond the current product metrics surface

Starter dashboard notes:

- lifecycle counters are controller-wide, not per-cluster
- autoscaling recommendation and signal panels are per `NiFiCluster` because those metrics expose `namespace` and `name` labels
- if you do not scrape the controller metrics endpoint yet, import the dashboard only after wiring that scrape path
- the dashboard intentionally does not assume exporter mode; it stays useful when `nativeApi` is the production path
- the runtime-health dashboard's native API scrape panel uses a starter `up{...}` query that assumes Prometheus Operator-style `namespace` and `service` labels; edit that query after import if your Prometheus labels differ
- the runtime-health dashboard's exporter panels only apply when `observability.metrics.mode=exporter` is enabled and the exporter's flow-status supplement remains enabled
- the runtime-health dashboard's queue-pressure panel still works from controller metrics even when exporter mode is not enabled

Expected first edits after import:

- point the Prometheus datasource prompt at your real datasource
- change the default `namespace` and `cluster` variables
- tune the runtime-health dashboard's native API `up{...}` query to match your actual Prometheus labels if needed
- move the dashboard into your preferred Grafana folder
- add any local panels for ingress, storage, external DNS, or identity-provider dependencies

## Local Import Proof

These starter JSON files were also smoke-tested against a local Grafana import flow:

1. Run a local Grafana container.
2. Create a Prometheus datasource.
3. Import both dashboard JSON files through the Grafana HTTP API.
4. Confirm Grafana accepts the dashboards without schema or panel-model errors.

The current local proof imported both dashboards successfully through the Grafana HTTP API on Grafana `12.4.2`.

This proof checks dashboard importability and model validity. It does not prove that your environment has the exact Prometheus labels or metrics mode assumptions the starter panels expect after import.
