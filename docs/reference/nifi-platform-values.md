# Platform Chart Values Reference

This page summarizes the real configuration surface of `charts/nifi-platform`.

File of record:

- `charts/nifi-platform/values.yaml`

## Top-Level Values

| Path | Default | Description |
| --- | --- | --- |
| `mode` | `standalone` | Selects standalone or managed platform behavior. |
| `controller.*` | see values file | Controller namespace, image, args, and resources. |
| `cluster.*` | see values file | `NiFiCluster` spec values rendered by the platform chart in managed mode. |
| `keda.*` | disabled | Optional experimental KEDA integration in managed mode only. |
| `nifi.*` | nested app-chart values | Full app chart values passed to `charts/nifi`. |

## `controller.*`

| Path | Description |
| --- | --- |
| `controller.namespace.name` | Namespace for the controller Deployment. |
| `controller.namespace.create` | Whether the namespace is created by the chart. |
| `controller.serviceAccount.name` | Optional controller ServiceAccount name override. |
| `controller.image.repository` | Controller image repository. |
| `controller.image.tag` | Controller image tag. |
| `controller.image.pullPolicy` | Controller image pull policy. |
| `controller.args[]` | Extra controller arguments. |
| `controller.resources.*` | Controller requests and limits. |

## `cluster.*`

These values map directly to the managed `NiFiCluster` resource rendered by the platform chart.

| Path | Description |
| --- | --- |
| `cluster.create` | Whether the platform chart creates a `NiFiCluster`. |
| `cluster.name` | Optional `NiFiCluster` name override. |
| `cluster.targetRef.name` | Optional explicit target `StatefulSet` name override. |
| `cluster.desiredState` | `Running` or `Hibernated`. |
| `cluster.suspend` | Suspend reconciliation. |
| `cluster.restartTriggers.*` | ConfigMaps and Secrets watched for restart decisions. |
| `cluster.restartPolicy.tlsDrift` | TLS drift policy. |
| `cluster.rollout.*` | Managed rollout timing values. |
| `cluster.hibernation.offloadTimeout` | Hibernation offload timeout. |
| `cluster.autoscaling.*` | Controller-owned autoscaling policy values. |
| `cluster.safety.requireClusterHealthy` | Cluster health gate for destructive actions. |

## `keda.*`

`keda.*` is optional and experimental.

| Path | Description |
| --- | --- |
| `keda.enabled` | Enables KEDA rendering in the platform chart. |
| `keda.pollingInterval` | KEDA polling interval. |
| `keda.cooldownPeriod` | KEDA cooldown period. |
| `keda.minReplicaCount` | Lower bound written through the KEDA/HPA path. |
| `keda.maxReplicaCount` | Upper bound written through the KEDA/HPA path. |
| `keda.triggers[]` | KEDA trigger definitions. |

## `nifi.*`

`nifi.*` is the nested app-chart configuration surface from `charts/nifi`.

Use [App Chart Values Reference](nifi-values.md) for the detailed section map.
