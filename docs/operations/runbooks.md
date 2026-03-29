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

## Temporary Pre-Start Inspection With `debugStartup`

Signals:

- the pod starts but you want to inspect rendered config or mounted inputs before `nifi.sh run`
- `debugStartup.enabled=true` is set in chart values
- the NiFi pod is intentionally `Running` but not `Ready`

Check:

```bash
kubectl -n nifi get pods
kubectl -n nifi describe pod nifi-0
kubectl -n nifi logs nifi-0 -c nifi --tail=50
kubectl -n nifi exec -it nifi-0 -c nifi -- /bin/sh
```

Respond:

- use this mode only as a temporary operator troubleshooting step
- inspect `/opt/nifi/nifi-current/conf`, mounted TLS material, extra trust bundles, and writable repository paths before the normal startup path resumes
- remember that the chart disables startup and liveness probes while the pause is active, but keeps readiness in place so the paused pod stays out of service
- if the cluster is controller-managed, expect rollout progress to remain blocked on the paused pod until the sleep window expires or the setting is reverted
- remove or disable `debugStartup` after inspection so the normal startup path can continue

## TLS Drift Escalated or Failed

Signals:

- `nifi_platform_tls_actions_total{action="observe_only",result="degraded"}`
- `nifi_platform_tls_actions_total{action="restart_required",result=~"started|failed"}`
- `status.tls.phase`
- `status.tls.reason`
- `status.tls.observationStartedAt`
- `status.rollout.trigger=TLSDrift`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.tls.phase}{" "}{.status.tls.reason}{"\n"}{.status.tls.message}{"\n"}{.status.tls.observationStartedAt}{"\n"}{.status.rollout.trigger}{"\n"}{.status.observedCertificateHash}{"\n"}{.status.observedTLSConfigurationHash}{"\n"}'
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

## Control-Plane Recovery After Namespace Loss

Signals:

- the NiFi namespace or Helm release was deleted or lost
- the operator has an exported control-plane backup bundle
- Secrets, issuers, and PVC recovery are being handled through the normal operator-owned paths

Check:

```bash
kubectl get ns nifi
kubectl get pvc -A | rg nifi
bash hack/recover-control-plane-backup.sh --backup-dir ./backup/nifi-control-plane
helm -n nifi status nifi
kubectl -n nifi get nificluster,statefulset,pods,svc,pvc
```

Respond:

- restore or recreate operator-owned Secrets, issuers, and any required PVCs before trusting the recovered release
- use the recovery helper to rebuild the declarative control plane, not to replace storage restore or Secret escrow
- if the namespace comes back but bounded runtime-owned features are still unhealthy, verify those after the base cluster is Ready rather than treating the initial Helm recovery as the final proof

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
- remember that exporter is GA but intentionally smaller in scope; if you are missing JVM or broader system-diagnostics families, that is outside the current GA claim rather than a scrape failure

## Flow-Action Audit Reporter Image Selection

Signals:

- `nifi.observability.audit.flowActions.export.type=log`
- chart values for `nifi.observability.audit.flowActions.export.log.installation.image.*`
- NiFi pod init-container failures around reporter installation
- missing `nifi.flowAction` events in the NiFi log stream

Check:

```bash
kubectl -n nifi get statefulset nifi -o jsonpath='{.spec.template.spec.initContainers[?(@.name=="install-flow-action-audit-reporter")].image}{"\n"}'
kubectl -n nifi get configmap nifi-config -o yaml | rg 'flow.action.reporter|nar.library.directory.flow.action.audit'
kubectl -n nifi logs nifi-0 -c install-flow-action-audit-reporter --tail=100
kubectl -n nifi logs nifi-0 -c nifi --tail=200 | rg 'nifi.flowAction|flow-action audit'
```

Respond:

- use `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:edge` only for branch-tip validation or pre-production soak
- use `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:sha-<commit>` when you need an immutable build tied to a reviewed commit
- use `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter:X.Y.Z` when you want the release-tagged build from `flow-action-audit-reporter-vX.Y.Z`
- if the cluster cannot reach GHCR or public registries, mirror the reporter image into the customer registry and set `nifi.observability.audit.flowActions.export.log.installation.image.repository` and `.tag` to that internal location
- if the customer is fully air-gapped, build the reporter image internally from the published source or release artifact and host it in the platform registry before enabling `export.type=log`
- this export path is optional; if the reporter image is not available yet, leave `export.type=disabled` and rely on NiFi-native local history, archive, and request log evidence
- keep the reporter image tag and the chart values pinned explicitly; do not rely on floating defaults in production
- if the init container cannot find the NAR at the configured path, first verify the image tag and only then override `narPath`
- if the reporter image is healthy but no audit events appear, treat local NiFi history, flow archive, and request log as the fallback evidence path

## KEDA Wants X, Controller Did Y

Signals:

- `spec.autoscaling.external.requestedReplicas`
- `status.autoscaling.external`
- `status.autoscaling.execution`
- `status.autoscaling.lastScalingDecision`
- `status.autoscaling.reason`
- `ScaledObject` and HPA status
- controller events and logs around autoscaling recommendation or execution

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.observed}{"\n"}{.status.autoscaling.external.source}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.boundedReplicas}{"\n"}{.status.autoscaling.external.actionable}{"\n"}{.status.autoscaling.external.scaleDownIgnored}{"\n"}{.status.autoscaling.external.reason}{"\n"}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.execution.phase}{" "}{.status.autoscaling.execution.state}{" "}{.status.autoscaling.execution.blockedReason}{" "}{.status.autoscaling.execution.failureReason}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}'
kubectl -n nifi get scaledobject nifi-keda -o yaml
kubectl -n nifi get hpa
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- confirm whether the controller actually observed the KEDA request
- compare the raw runtime-managed request, the effective request after controller min and max checks, and the final controller decision before assuming the controller is wrong
- if the controller adjusted the request, inspect autoscaling min and max first
- if the controller deferred or blocked the request, inspect higher-precedence lifecycle work before changing autoscaling settings
- treat `lastScalingDecision` as the support summary for why KEDA wanted one size and the controller applied another

## KEDA Downscale Request Ignored or Blocked

Signals:

- `status.autoscaling.external.scaleDownIgnored`
- `status.autoscaling.external.reason`
- `status.autoscaling.lastScalingDecision`
- `spec.autoscaling.external.scaleDownEnabled`
- `spec.autoscaling.minReplicas`

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.autoscaling.external.scaleDownEnabled}{"\n"}{.spec.autoscaling.minReplicas}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.boundedReplicas}{"\n"}{.status.autoscaling.external.scaleDownIgnored}{"\n"}{.status.autoscaling.external.reason}{"\n"}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- if `scaleDownEnabled=false`, treat refusal as expected behavior
- if the requested size is already at or below `minReplicas`, treat refusal as expected floor enforcement
- if external downscale was enabled but execution still blocked, treat that as supported controller-mediated behavior and inspect the normal controller-owned safe scale-down checks rather than KEDA itself
- do not expect KEDA or the generated HPA to remove a pod directly

## KEDA Scale Request Blocked by Lifecycle Precedence

Signals:

- `status.autoscaling.external.reason`
- `status.autoscaling.execution.state`
- `status.autoscaling.lastScalingDecision`
- rollout, TLS, hibernation, restore, and degraded status fields

Check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.autoscaling.external.reason}{"\n"}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.execution.state}{" "}{.status.autoscaling.execution.blockedReason}{" "}{.status.autoscaling.execution.message}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}{.status.rollout.trigger}{"\n"}{.status.lastOperation.type}{" "}{.status.lastOperation.phase}{" "}{.status.lastOperation.message}{"\n"}{range .status.conditions[*]}{.type}{": "}{.reason}{" "}{.status}{"\n"}{end}'
kubectl -n nifi describe nificluster nifi
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- confirm whether rollout, TLS restart, hibernation, restore, or degraded-state handling took precedence over the KEDA request
- let the higher-precedence controller work finish before forcing autoscaling changes
- if the cluster stays blocked longer than your accepted recovery window, escalate it as a lifecycle issue first and a KEDA issue second

## GitOps Keeps Reverting Runtime-Managed External Intent

Signals:

- `spec.autoscaling.external.requestedReplicas` repeatedly returns to `0` or another declared value
- `status.autoscaling.external.requestedReplicas` continues to show KEDA-originated requests
- GitOps controller or policy-engine drift reports
- controller logs showing repeated observed external intent without durable steady-state

Check:

```bash
helm -n nifi get values nifi
kubectl -n nifi get nificluster nifi -o yaml
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Respond:

- keep declarative values at `cluster.autoscaling.external.requestedReplicas=0` when `keda.enabled=true`
- configure your GitOps reconciler to ignore or accept drift on `spec.autoscaling.external.requestedReplicas`
- do not hand-author the runtime-managed field in Helm values, Kustomize patches, or policy defaults
- if the GitOps tool cannot ignore that field, do not claim KEDA is enabled for that environment until the reconcile policy is fixed
