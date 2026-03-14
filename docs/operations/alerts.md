# Operations Alerts

NiFi-Fabric now includes one small starter Prometheus rules file:

- [Prometheus starter alert rules YAML](../../ops/prometheus/nifi-fabric-starter-alerts.yaml)

What the starter rules cover directly:

- managed rollout failure
- rollout node-preparation timeout as a practical blocked-rollout signal
- TLS observe-only degradation
- TLS restart-required escalation
- TLS restart-required failure
- hibernation failure
- hibernation node-preparation timeout as a practical blocked-hibernation signal
- autoscaling execution blocked
- autoscaling execution failed
- exporter upstream source failure when exporter mode is enabled and scraped

What stays guidance-first instead of hard-coded rules:

- long-running restore blockage
- KEDA external intent ignored versus actionable
- native API scrape target `up` alerts

Those cases are still important, but the exact query shape depends on whether your environment exports `NiFiCluster` custom-resource status through kube-state-metrics and how your Prometheus jobs label the app-chart metrics targets.

Recommended operator follow-up:

- tune the alert windows and severities for your paging policy
- add `up` or `absent()` alerts that match your real Prometheus job labels
- add cluster-specific rules if you expose `NiFiCluster` status through kube-state-metrics
- route experimental exporter alerts differently from `nativeApi` alerts if you run both modes

KEDA note:

- the product already records autoscaling recommendation and execution signals
- the current starter file does not hard-code a KEDA-specific alert because external-intent labels are not exposed as a stable controller metric today
- use the runbook and `status.autoscaling.external` as the first operator check when KEDA intent appears blocked or ignored
