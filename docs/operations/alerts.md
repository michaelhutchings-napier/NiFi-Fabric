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
- KEDA external intent ignored, blocked, deferred, or GitOps-conflicted
- native API scrape target `up` alerts

Those cases are still important, but the exact query shape depends on whether your environment exports `NiFiCluster` custom-resource status through kube-state-metrics and how your Prometheus jobs label the app-chart metrics targets.

Recommended operator follow-up:

- tune the alert windows and severities for your paging policy
- add `up` or `absent()` alerts that match your real Prometheus job labels
- add cluster-specific rules if you expose `NiFiCluster` status through kube-state-metrics
- keep exporter alerts on a separate route from `nativeApi` alerts if you run both modes so operators can see which metrics mode is failing

KEDA note:

- the product already records autoscaling recommendation and execution signals
- controller-mediated KEDA external downscale is now part of the supported path, but it still uses the same bounded safe scale-down semantics
- the current starter file does not hard-code a KEDA-specific alert because external-intent labels are not exposed as a stable controller metric today
- use the runbook and `status.autoscaling.external` as the first operator check when KEDA intent appears blocked, deferred, ignored, or conflicted by GitOps

KEDA guidance-first alert targets:

- `intent ignored for too long`
  Watch `status.autoscaling.external.observed`, `status.autoscaling.external.actionable`, `status.autoscaling.external.scaleDownIgnored`, `status.autoscaling.external.reason`, and `status.autoscaling.lastScalingDecision`. Alert only when the same ignored state persists beyond one normal polling or cooldown window for your environment.
- `intent blocked for too long`
  Watch `status.autoscaling.execution.state`, `status.autoscaling.execution.blockedReason`, `status.autoscaling.external.reason`, and `status.autoscaling.lastScalingDecision`. This is the right alert when KEDA asked for capacity but a real controller step stayed blocked too long.
- `intent blocked by lifecycle precedence for too long`
  Watch the same external-intent fields together with rollout, TLS, hibernation, restore, or degraded signals. This should page only after the higher-precedence activity has exceeded your accepted maintenance or recovery window.
- `downscale refused repeatedly`
  Watch repeated `scaleDownIgnored=true` states, repeated `ExternalScaleDownMinimumSatisfied`-style reasons, or repeated controller events showing that KEDA asked below floor or while external downscale was not actionable.
- `runtime-managed field drift or GitOps conflict`
  Alert from your GitOps controller, policy engine, or drift tooling when `spec.autoscaling.external.requestedReplicas` is repeatedly reconciled away from the runtime value. The product starter rules do not hard-code this because the signal comes from your GitOps stack, not from a stable built-in NiFi-Fabric metric.

Recommended implementation sources:

- if you export `NiFiCluster` status through kube-state-metrics, build environment-specific alerts from the `status.autoscaling.external.*`, `status.autoscaling.execution.*`, and lifecycle status fields
- if you do not export CR status, prefer controller-event or log-based alerts for blocked or ignored intent and GitOps-native alerts for drift on the runtime-managed field
- keep KEDA alerts separate from the standard built-in autoscaling alert route so operators can distinguish external intent handling from controller-native recommendation or execution issues
