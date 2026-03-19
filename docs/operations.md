# Operations and Troubleshooting

This is the customer-facing starter operations package for NiFi-Fabric.

It stays intentionally small:

- one starter Grafana dashboard
- one starter Prometheus alert rules file
- concise runbooks for the most common platform failure modes

What it covers:

- controller lifecycle signals for rollout, TLS drift, hibernation, restore, and autoscaling
- metrics subsystem checks for the currently supported metrics modes
- disaster-recovery planning boundaries and backup or restore posture links
- operator-facing troubleshooting steps built around `NiFiCluster` status, controller events, controller metrics, and chart-owned metrics resources

What operators still need to adapt:

- Prometheus scrape job labels, namespaces, and alert routing
- Grafana datasource names and dashboard folder conventions
- severity thresholds, notification policies, and maintenance silences
- any environment-specific dashboards for ingress, storage, cloud load balancers, or external identity systems

## Included Assets

- [Starter dashboards](operations/dashboards.md)
- [Starter alerts](operations/alerts.md)
- [Starter runbooks](operations/runbooks.md)
- [Grafana starter dashboard JSON](../ops/grafana/nifi-fabric-starter-dashboard.json)
- [Prometheus starter alert rules YAML](../ops/prometheus/nifi-fabric-starter-alerts.yaml)

## Fast Checks

Helm release status:

```bash
helm -n nifi status nifi
```

Platform resources:

```bash
kubectl -n nifi get nificluster,statefulset,pods,svc
kubectl -n nifi-system get deployment,pods
```

NiFiCluster status:

```bash
kubectl -n nifi get nificluster nifi -o yaml
```

Controller logs:

```bash
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Controller metrics quick check:

```bash
kubectl -n nifi-system port-forward deployment/nifi-controller-manager 18080:8080
curl --silent http://127.0.0.1:18080/metrics | rg '^nifi_platform_'
```

Exporter metrics quick check for the bounded supported profile:

```bash
kubectl -n nifi get deployment nifi-metrics-exporter -o jsonpath='{.status.readyReplicas}{"\n"}'
kubectl -n nifi get service nifi-exporter -o yaml
kubectl -n nifi get servicemonitor nifi-exporter -o yaml
kubectl -n nifi logs deployment/nifi-metrics-exporter --tail=100
kubectl -n nifi port-forward deployment/nifi-metrics-exporter 19090:9090
curl --silent http://127.0.0.1:19090/readyz && echo
curl --silent http://127.0.0.1:19090/metrics | rg 'nifi_fabric_exporter_source_up|nifi_amount_running_components'
```

Reading those checks:

- exporter mode is optional; verify the release actually uses `observability.metrics.mode=exporter` before debugging it
- `nativeApi` remains the primary recommended metrics path even when exporter mode is supported
- the exporter `Deployment`, `Service`, and `ServiceMonitor` should all exist together in the bounded GA profile
- `/readyz` should reflect secured upstream NiFi reachability, not just local process liveness
- `/metrics` should expose exporter self-diagnostics plus the bounded relayed NiFi metric families documented for this mode

Site-to-Site metrics quick check for the bounded supported profile:

```bash
kubectl -n nifi get configmap nifi-site-to-site-metrics -o jsonpath='{.data.config\.json}' | python3 -m json.tool
kubectl -n nifi logs nifi-0 -c nifi --tail=200 | rg 'site-to-site|SiteToSiteMetricsReportingTask|fabric-site-to-site-metrics'
kubectl -n nifi exec nifi-0 -c nifi -- test -f /opt/nifi/fabric/site-to-site-metrics/config.json && echo mounted
```

Reading those checks:

- `siteToSite` is optional; verify the release actually uses `observability.metrics.mode=siteToSite` and `observability.metrics.siteToSite.enabled=true`
- `nativeApi` still remains the primary recommended metrics path even when the bounded Site-to-Site path is GA
- the mounted config should show the declared destination URL, input port name, auth mode, and authorized identity contract
- the bounded GA path owns exactly one typed reporting task and one bounded SSL context service shape when secure transport is used
- receiver topology, trust, and destination-side authz lifecycle remain operator-owned even though the sender-side path is GA

Linkerd quick check for the bounded supported profile:

```bash
linkerd check --proxy --namespace nifi
kubectl -n nifi get statefulset nifi -o jsonpath='{.spec.template.metadata.annotations.linkerd\.io/inject}{"\n"}{.spec.template.metadata.annotations.config\.linkerd\.io/opaque-ports}{"\n"}'
kubectl -n nifi get service nifi-headless -o jsonpath='{.metadata.annotations.config\.linkerd\.io/opaque-ports}{"\n"}'
kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name
```

Reading those checks:

- `linkerd check --proxy --namespace nifi` confirms that the NiFi data plane proxies are healthy
- the StatefulSet pod template should show `linkerd.io/inject=enabled`
- the bounded baseline profile should show `config.linkerd.io/opaque-ports` for the NiFi cluster and load-balance ports
- the headless Service should carry the same opaque-port annotation for the supported internal NiFi TCP ports
- each meshed NiFi pod should list `linkerd-proxy` as an additional container

Istio quick check for the bounded supported sidecar-mode profile:

```bash
kubectl get namespace nifi -o jsonpath='{.metadata.labels.istio-injection}{"\n"}'
kubectl -n nifi get statefulset nifi -o jsonpath='{.spec.template.metadata.annotations.sidecar\.istio\.io/inject}{"\n"}{.spec.template.metadata.annotations.sidecar\.istio\.io/rewriteAppHTTPProbers}{"\n"}{.spec.template.metadata.annotations.proxy\.istio\.io/config}{"\n"}'
kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name
kubectl -n nifi-system get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name
```

Reading those checks:

- the NiFi namespace should show `istio-injection=enabled`
- the StatefulSet pod template should show `sidecar.istio.io/inject="true"`
- the bounded baseline profile should show `sidecar.istio.io/rewriteAppHTTPProbers="true"`
- the bounded baseline profile should also show `proxy.istio.io/config` with `holdApplicationUntilProxyStarts`
- each meshed NiFi pod should list `istio-proxy` as an additional container
- the controller pod should not list `istio-proxy`

Istio Ambient quick check for the bounded supported profile:

```bash
kubectl get namespace nifi -o jsonpath='{.metadata.labels.istio-injection}{" "}{.metadata.labels.istio\.io/dataplane-mode}{"\n"}'
kubectl -n nifi get statefulset nifi -o jsonpath='{.spec.template.metadata.labels.istio\.io/dataplane-mode}{"\n"}'
kubectl -n nifi get pods -o custom-columns=NAME:.metadata.name,LABEL:.metadata.labels.istio\\.io/dataplane-mode,CONTAINERS:.spec.containers[*].name
kubectl -n nifi-system get pods -o custom-columns=NAME:.metadata.name,CONTAINERS:.spec.containers[*].name
kubectl -n istio-system get daemonset ztunnel
```

Reading those checks:

- the supported overlay does not require `istio-injection=enabled` on the NiFi namespace
- the StatefulSet pod template should show `istio.io/dataplane-mode=ambient`
- each Ambient-enabled NiFi pod should show the same label and should not list `istio-proxy`
- the controller pod should remain plain and should not list `istio-proxy`
- `ztunnel` should be ready for the bounded Ambient profile

Autoscaling blocked-state quick check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.autoscaling.mode}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.recommendedReplicas}{"\n"}{.status.autoscaling.execution.phase}{" "}{.status.autoscaling.execution.state}{" "}{.status.autoscaling.execution.blockedReason}{" "}{.status.autoscaling.execution.failureReason}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}'
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.autoscaling.external.reason}{" "}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.execution.message}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{"\n"}'
```

KEDA intent quick check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.observed}{"\n"}{.status.autoscaling.external.source}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.external.boundedReplicas}{"\n"}{.status.autoscaling.external.actionable}{"\n"}{.status.autoscaling.external.scaleDownIgnored}{"\n"}{.status.autoscaling.external.reason}{"\n"}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}'
kubectl -n nifi get scaledobject nifi-keda -o yaml
kubectl -n nifi get hpa
kubectl -n nifi get events --field-selector involvedObject.kind=NiFiCluster,involvedObject.name=nifi --sort-by=.lastTimestamp | rg 'AutoscalingExternalIntent|AutoscalingExecution'
kubectl -n nifi-system logs deployment/nifi-controller-manager --tail=200
```

Reading those fields:

- `spec.autoscaling.mode` is the configured control mode. `Advisory` keeps recommendation-only behavior; `Enforced` allows the controller to execute scale-up and bounded sequential scale-down work.
- `status.autoscaling.external.requestedReplicas` is the last external request the controller observed, for example from KEDA through `/scale`.
- `status.autoscaling.recommendedReplicas` is the controller's current bounded recommendation after applying policy limits and signal evaluation.
- `status.autoscaling.execution.phase`, `state`, `plannedSteps`, `completedSteps`, `blockedReason`, `failureReason`, and `message` describe the live execution checkpoint when autoscaling is actively settling or blocked.
- `status.autoscaling.lastScalingDecision` now carries the operator-facing summary for allowed, blocked, deferred, ignored, or failed decisions and appends context for mode, current size, recommendation, request, and active execution when relevant.
- `status.nodeOperation` shows which pod and destructive preparation stage are active during safe scale-down.
- for blocked one-step scale-down, the execution and decision text now also explain whether the actual StatefulSet removal pod was selected, rejected because it is missing, rejected because it is already terminating, or rejected because it is not Ready, and why lower ordinals were not chosen instead
- when KEDA is enabled, `spec.autoscaling.external.requestedReplicas` is the runtime-managed `/scale` input and `status.autoscaling.external.requestedReplicas` is the last value the controller actually observed
- if KEDA wants one size and the controller applies another, trust `status.autoscaling.external.*`, `status.autoscaling.execution.*`, and `lastScalingDecision` over the raw KEDA request alone
- if `scaleDownIgnored=true`, the controller received the KEDA downscale request but intentionally refused it under the bounded safe rules
- `AutoscalingExternalIntentBlocked` events mean controller-owned lifecycle or destructive work took precedence; `AutoscalingExternalIntentDeferred` means the request is still waiting on cooldown, low-pressure evidence, or stabilization without a higher-precedence conflict
- if the external request keeps snapping back to `0` or another declarative value, suspect GitOps reconciliation fighting the runtime-managed field
- controller restart should not erase the KEDA request; after restart, verify the persisted `/scale` input plus rebuilt `status.autoscaling.external.*` before assuming intent was lost

Support position:

- `Advisory` is the production-ready bounded recommendation path
- `Enforced` scale-up is the production-ready bounded execution path
- `Enforced` scale-down is production-ready for the bounded controller-owned sequential one-node path, including bounded sequential multi-step episodes
- the richer built-in policy depth is part of that supported bounded model: confidence-based scale-up, bounded capacity reasoning, actual removal-candidate qualification, and restart-safe sequential scale-down execution
- GA: KEDA external scale-up intent is supported and secondary to the built-in autoscaler
- KEDA support remains bounded to external intent through `NiFiCluster` `/scale`; KEDA is not the executor and does not own `StatefulSet.spec.replicas`
- GA: opt-in KEDA external downscale is supported only through controller mediation and the normal safe scale-down policy

When controller-owned scale-down is stalled, expect:

- `status.autoscaling.execution.state=Blocked`
- a stage-specific `blockedReason` such as disconnect retrying, offload timed out, drain pending, drain stalled, ready-pod pending, or health-gate timed out
- bounded sequential episodes can also block between steps on cooldown or stabilization before the next one-node removal is re-qualified
- precedence pauses now also surface explicitly, for example rollout, restore, or hibernation taking over a previously started scale-down step
- `lastScalingDecision` and `execution.message` to explain why the step is blocked, whether the controller is waiting or needs operator intervention, and what to inspect next

Operator checks for a stalled autoscaling removal step:

- inspect the actual StatefulSet `N -> N-1` removal pod named in `lastScalingDecision` and any terminating pod with `kubectl -n nifi get pod -o wide`
- inspect `status.nodeOperation` and the autoscaling execution block or timeout reason on `NiFiCluster`
- inspect controller logs and recent events for the same pod or node id
- inspect NiFi node state through the UI or API to confirm whether the target node is stuck disconnecting, disconnected, or offloading
- if the blocked reason shows a higher-precedence lifecycle pause, inspect the rollout, TLS, hibernation, or restore status first; autoscaling should resume only after that work clears
- treat failed execution as operator-owned intervention; blocked execution remains resumable on the next reconcile or controller restart

KEDA operator expectations:

- KEDA may publish a request that the controller later bounds, defers, blocks, or ignores
- rollout, TLS, hibernation, restore, degraded state, cooldown, and low-pressure rules still take precedence over KEDA intent
- already-running destructive autoscaling work can also block a new KEDA request until that controller-owned step settles or resumes safely
- KEDA external scale-up and controller-mediated external downscale are the GA paths; downscale still remains conservative, so the controller may keep the current size when the request is disabled, below floor, unsafe, or still waiting on the normal safe checks
- after a controller restart, the persisted KEDA request should remain visible on `NiFiCluster` and the controller should rebuild the same blocked, deferred, or actionable interpretation on the next reconcile
- GitOps users must treat `spec.autoscaling.external.requestedReplicas` as runtime-managed when KEDA is enabled

## Backup and DR

Use the dedicated DR guide for production backup and restore expectations:

- [Backup, Restore, and Disaster Recovery](dr.md)

Recommended operator pattern:

- treat Helm values, overlays, and `NiFiCluster` as the control-plane backup scope
- treat NiFi repository PVCs as the data-plane recovery scope
- rehearse redeploy-only recovery and snapshot-backed recovery as separate runbooks

Quick DR posture checks:

```bash
helm -n nifi get values nifi
kubectl -n nifi get nificluster nifi -o yaml
kubectl -n nifi get pvc
kubectl -n nifi get secret,configmap
```

Control-plane export:

```bash
bash hack/export-control-plane-backup.sh \
  --release nifi \
  --namespace nifi \
  --output-dir ./backup/nifi-control-plane
```

Control-plane recovery:

```bash
bash hack/recover-control-plane-backup.sh \
  --backup-dir ./backup/nifi-control-plane
```

Focused bounded restore workflow proof:

```bash
make kind-platform-managed-restore-fast-e2e
```

What that focused proof exercises:

- reinstall through `charts/nifi-platform`
- control-plane recovery through the backup bundle helper
- runtime Flow Registry Client reconnect from the restored catalog
- runtime-managed Parameter Context reconciliation from restored config
- bounded runtime-managed import of the selected registry-backed flow and direct Parameter Context attachment after the restored release starts
- no PVC-backed queue or repository replay

## Support Boundary

NiFi-Fabric is intentionally conservative about support claims:

- kind is the runtime proof baseline in this repository
- AKS and OpenShift guidance is published separately and remains conservative because no real cluster was exercised in this slice
- `nativeApi` remains the production-ready metrics path
- `exporter` is GA only within the bounded documented metrics scope and remains optional
- `siteToSite` is GA only within the bounded documented sender-side metrics scope and remains optional
- typed Site-to-Site sender features remain bounded and do not become a generic recovery or runtime-object framework
- DR guidance documents a production posture, but the product does not claim storage snapshot orchestration or full NiFi internal recovery ownership
- the starter operations assets are templates, not a production certification pack
