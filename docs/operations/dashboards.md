# Operations Dashboards

NiFi-Fabric now includes one small starter dashboard:

- [Grafana starter dashboard JSON](../../ops/grafana/nifi-fabric-starter-dashboard.json)

What it covers:

- controller lifecycle transition rate
- rollout start, completion, and failure counts
- TLS observe-only versus restart-required actions
- hibernation and restore operation counts
- node-preparation retries and timeouts
- autoscaling execution transitions
- autoscaling recommended replicas
- autoscaling queue-pressure and CPU sample signals collected from NiFi

Why the first dashboard stays small:

- it focuses on signals the product already exposes
- it does not assume a large dashboard pack or opinionated folder structure
- it avoids overclaiming environment support beyond the current product metrics surface

Starter dashboard notes:

- lifecycle counters are controller-wide, not per-cluster
- autoscaling recommendation and signal panels are per `NiFiCluster` because those metrics expose `namespace` and `name` labels
- if you do not scrape the controller metrics endpoint yet, import the dashboard only after wiring that scrape path
- the dashboard intentionally does not assume exporter mode; it stays useful when `nativeApi` is the production path

Expected first edits after import:

- point the Prometheus datasource prompt at your real datasource
- change the default `namespace` and `cluster` variables
- move the dashboard into your preferred Grafana folder
- add any local panels for ingress, storage, external DNS, or identity-provider dependencies
