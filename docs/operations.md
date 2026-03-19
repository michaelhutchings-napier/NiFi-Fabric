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

Autoscaling blocked-state quick check:

```bash
kubectl -n nifi get nificluster nifi -o jsonpath='{.spec.autoscaling.mode}{"\n"}{.status.autoscaling.external.requestedReplicas}{"\n"}{.status.autoscaling.recommendedReplicas}{"\n"}{.status.autoscaling.execution.phase}{" "}{.status.autoscaling.execution.state}{" "}{.status.autoscaling.execution.blockedReason}{" "}{.status.autoscaling.execution.failureReason}{"\n"}{.status.autoscaling.lastScalingDecision}{"\n"}'
kubectl -n nifi get nificluster nifi -o jsonpath='{.status.autoscaling.external.reason}{" "}{.status.autoscaling.external.message}{"\n"}{.status.autoscaling.execution.message}{"\n"}{.status.nodeOperation.podName}{" "}{.status.nodeOperation.stage}{"\n"}'
```

Reading those fields:

- `spec.autoscaling.mode` is the configured control mode. `Advisory` keeps recommendation-only behavior; `Enforced` allows the controller to execute scale-up and bounded sequential scale-down work.
- `status.autoscaling.external.requestedReplicas` is the last external request the controller observed, for example from KEDA through `/scale`.
- `status.autoscaling.recommendedReplicas` is the controller's current bounded recommendation after applying policy limits and signal evaluation.
- `status.autoscaling.execution.phase`, `state`, `plannedSteps`, `completedSteps`, `blockedReason`, `failureReason`, and `message` describe the live execution checkpoint when autoscaling is actively settling or blocked.
- `status.autoscaling.lastScalingDecision` now carries the operator-facing summary for allowed, blocked, deferred, ignored, or failed decisions and appends context for mode, current size, recommendation, request, and active execution when relevant.
- `status.nodeOperation` shows which pod and destructive preparation stage are active during safe scale-down.
- for blocked one-step scale-down, the execution and decision text now also explain whether the actual StatefulSet removal pod was selected, rejected because it is missing, rejected because it is already terminating, or rejected because it is not Ready, and why lower ordinals were not chosen instead

Support position:

- `Advisory` is the production-ready bounded recommendation path
- `Enforced` scale-up is the production-ready bounded execution path
- `Enforced` scale-down is production-ready for the bounded controller-owned sequential one-node path, including bounded sequential multi-step episodes
- the richer built-in policy depth is part of that supported bounded model: confidence-based scale-up, bounded capacity reasoning, actual removal-candidate qualification, and restart-safe sequential scale-down execution
- optional KEDA external intent remains experimental and secondary to the built-in autoscaler

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
- `exporter` remains experimental even though focused runtime proof exists
- typed Site-to-Site sender features remain bounded and do not become a generic recovery or runtime-object framework
- DR guidance documents a production posture, but the product does not claim storage snapshot orchestration or full NiFi internal recovery ownership
- the starter operations assets are templates, not a production certification pack
