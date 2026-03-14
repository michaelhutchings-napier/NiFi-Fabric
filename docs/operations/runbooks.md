# Operations Runbooks

These runbooks stay short on purpose. They are the first-response path, not a giant operations manual.

## Managed Rollout Blocked or Failed

Signals:

- `nifi_platform_rollouts_total{result="failed"}`
- `nifi_platform_node_preparation_outcomes_total{purpose="Restart",result=~"retrying|timed_out"}`
- `ConditionProgressing`
- `ConditionDegraded`
- `status.rollout.trigger`
- `status.nodeOperation`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.rollout.trigger}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi get pods -o wide
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
kubectl -n nifi get events --field-selector involvedObject.kind=NiFiCluster,involvedObject.name=nifi --sort-by=.lastTimestamp
```

Respond:

- confirm whether the trigger is revision drift, config drift, or TLS drift
- if node preparation is stuck, look for NiFi disconnect or offload failures before deleting anything manually
- if the controller already marked the cluster degraded, stop automated retries until the underlying NiFi or storage issue is understood

## TLS Drift Escalated or Failed

Signals:

- `nifi_platform_tls_actions_total{action="observe_only",result="degraded"}`
- `nifi_platform_tls_actions_total{action="restart_required",result=~"started|failed"}`
- `status.tls.observationStartedAt`
- `status.rollout.trigger=TLSDrift`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.tls.observationStartedAt}{"\n"}{.status.rollout.trigger}{"\n"}{.status.observedCertificateHash}{"\n"}{.status.observedTLSConfigurationHash}{"\n"}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi get secret nifi-tls -o yaml
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- decide whether the drift should have resolved through NiFi autoreload or required restart
- verify the mounted TLS Secret actually changed and that the workload saw the new material
- if restart-required failed, treat it like a rollout failure and use the rollout runbook

## Hibernation or Restore Blocked or Failed

Signals:

- `nifi_platform_hibernation_operations_total`
- `nifi_platform_node_preparation_outcomes_total{purpose="Hibernation",result=~"retrying|timed_out"}`
- `ConditionHibernated`
- `status.hibernation.lastRunningReplicas`
- `status.lastOperation`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.desiredState}{"\n"}{.status.hibernation.lastRunningReplicas}{"\n"}{.status.lastOperation.type}{" "}{.status.lastOperation.phase}{" "}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi get pods
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- verify whether the cluster is still in the NiFi preparation path or already in the Kubernetes scale step
- if restore is slow, confirm pods are becoming Ready and the NiFi cluster health gate is advancing
- if hibernation failed, inspect the target pod and any storage or offload symptoms before retrying

## Autoscaling Blocked or Failed

Signals:

- `nifi_platform_autoscaling_execution_transitions_total{state=~"Blocked|Failed"}`
- `nifi_platform_autoscaling_recommendations_total{outcome="blocked"}`
- `nifi_platform_autoscaling_recommended_replicas`
- `nifi_platform_autoscaling_signal_sample`
- `status.autoscaling`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.autoscaling.reason}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}{.status.autoscaling.execution.phase}{" "}{.status.autoscaling.execution.state}{" "}{.status.autoscaling.execution.blockedReason}{" "}{.status.autoscaling.execution.failureReason}{"\n"}{.status.autoscaling.execution.message}{"\n"}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- distinguish advisory recommendation issues from enforced execution issues
- if execution is blocked, check cooldown, stabilization, and lifecycle-precedence conditions first
- if execution failed, inspect the highest-ordinal pod preparation path and the target StatefulSet update

## Metrics Subsystem Failure

Signals:

- missing or failing `ServiceMonitor` targets
- exporter `/readyz` or `/metrics` failures when exporter mode is enabled
- `nifi_fabric_exporter_source_up` when exporter mode is scraped
- chart-owned metrics `Service` and `ServiceMonitor` objects

Check:

```bash
kubectl -n nifi get service,servicemonitor
kubectl -n nifi get deployment nifi-metrics-exporter -o yaml
kubectl -n nifi logs deployment/nifi-metrics-exporter --tail=200
kubectl -n nifi get secret nifi-metrics-auth -o yaml
kubectl -n nifi get secret nifi-metrics-ca -o yaml
```

Respond:

- confirm the selected metrics mode before debugging the scrape path
- for `nativeApi`, verify the machine-auth Secret, CA material, and ServiceMonitor TLS/auth references
- for exporter mode, verify upstream reachability, mounted auth and CA material, and exporter self-metrics
- keep exporter alerts and dashboards clearly marked experimental if you use that mode in production

## KEDA External Intent Ignored

Signals:

- `status.autoscaling.external`
- `status.autoscaling.reason`
- controller events and logs around autoscaling recommendation or execution

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.autoscaling.external.observed}{"\n"}{.status.autoscaling.external.source}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.actionable}{"\n"}{.status.autoscaling.external.scaleDownIgnored}{"\n"}{.status.autoscaling.external.reason}{"\n"}{.status.autoscaling.external.message}{"\n"}'
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- confirm whether the request was observed but intentionally ignored because lifecycle safety took precedence
- treat blocked or ignored external intent as secondary to rollout, TLS, and hibernation safety
- keep KEDA-specific routing separate from the standard platform alert path because KEDA remains experimental
