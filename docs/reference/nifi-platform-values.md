# Platform Chart Values Reference

This page summarizes the customer-facing configuration surface of `charts/nifi-platform`.

File of record:

- `charts/nifi-platform/values.yaml`

For install steps, see [Install with Helm](../install/helm.md). For managed lifecycle behavior, see [Hibernation and Restore](../manage/hibernation-and-restore.md), [Autoscaling](../manage/autoscaling.md), and [Observability and Metrics](../manage/observability.md).

## Install Mode

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `mode` | enum | Install mode. Values: `standalone`, `managed`. | No | `standalone` |

## Controller Settings

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `controller.namespace.name` | string | Namespace for the controller Deployment. | No | `nifi-system` |
| `controller.namespace.create` | boolean | Creates the controller namespace when `true`. | No | `true` |
| `controller.serviceAccount.name` | string | Existing ServiceAccount name to reuse. Leave empty for the chart-managed default. | No | `""` |
| `controller.image.repository` | string | Controller image repository. | No | `nifi-fabric-controller` |
| `controller.image.tag` | string | Controller image tag. | No | `dev` |
| `controller.image.pullPolicy` | string | Controller image pull policy. | No | `IfNotPresent` |
| `controller.args[]` | string list | Extra controller arguments. | No | `["--leader-elect"]` |
| `controller.resources.requests.cpu` | string | Controller CPU request. | No | `100m` |
| `controller.resources.requests.memory` | string | Controller memory request. | No | `128Mi` |
| `controller.resources.limits.cpu` | string | Controller CPU limit. | No | `500m` |
| `controller.resources.limits.memory` | string | Controller memory limit. | No | `512Mi` |

## Managed NiFiCluster Settings

These values render the managed `NiFiCluster` resource when `mode=managed`.

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `cluster.create` | boolean | Creates the `NiFiCluster` resource. | No | `true` |
| `cluster.name` | string | `NiFiCluster` name override. Leave empty to use the release name. | No | `""` |
| `cluster.targetRef.name` | string | Explicit target `StatefulSet` name override. | No | `""` |
| `cluster.desiredState` | enum | Desired high-level state. Values: `Running`, `Hibernated`. | No | `Running` |
| `cluster.suspend` | boolean | Suspends reconciliation while keeping the resource. | No | `false` |
| `cluster.restartTriggers.configMaps[]` | object list | ConfigMaps watched for restart decisions. | No | `[]` |
| `cluster.restartTriggers.secrets[]` | object list | Secrets watched for restart decisions. | No | `[]` |
| `cluster.restartPolicy.tlsDrift` | enum | TLS drift policy. | No | `AutoreloadThenRestartOnFailure` |
| `cluster.rollout.minReadySeconds` | integer | Minimum ready time before rollout advancement. | No | `30` |
| `cluster.rollout.podReadyTimeout` | duration | Per-pod rollout timeout. | No | `10m` |
| `cluster.rollout.clusterHealthTimeout` | duration | Cluster settle timeout during rollout and restart flows. | No | `15m` |
| `cluster.hibernation.offloadTimeout` | duration | Maximum offload wait during hibernation or safe scale-down. | No | `5m` |
| `cluster.safety.requireClusterHealthy` | boolean | Requires a healthy cluster before destructive actions. | No | `true` |

## Managed Autoscaling

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `cluster.autoscaling.mode` | enum | Autoscaling mode. Values: `Disabled`, `Advisory`, `Enforced`. | No | `Disabled` |
| `cluster.autoscaling.scaleUp.enabled` | boolean | Enables controller-owned scale-up in enforced mode. | No | `false` |
| `cluster.autoscaling.scaleUp.cooldown` | duration | Minimum time between scale-up actions. | No | `5m` |
| `cluster.autoscaling.scaleDown.enabled` | boolean | Enables controller-owned one-step safe scale-down. Experimental. | No | `false` |
| `cluster.autoscaling.scaleDown.cooldown` | duration | Minimum time between scale-down actions. | No | `10m` |
| `cluster.autoscaling.scaleDown.stabilizationWindow` | duration | Required low-pressure stability before scale-down. | No | `5m` |
| `cluster.autoscaling.external.enabled` | boolean | Enables the external intent surface used by optional KEDA integration. Experimental. | No | `false` |
| `cluster.autoscaling.external.source` | string | External source name. Current supported value is `KEDA`. Experimental. | No | `""` |
| `cluster.autoscaling.external.scaleDownEnabled` | boolean | Allows best-effort external downscale intent to be considered. Experimental. | No | `false` |
| `cluster.autoscaling.external.requestedReplicas` | integer | External requested replicas observed through `/scale`. | No | `0` |
| `cluster.autoscaling.minReplicas` | integer | Lower autoscaling bound. | No | `0` |
| `cluster.autoscaling.maxReplicas` | integer | Upper autoscaling bound. | No | `0` |
| `cluster.autoscaling.signals[]` | string list | Signals the controller may evaluate. | No | `[]` |

## Experimental KEDA Integration

`keda.*` is optional and experimental. For behavior details, see [KEDA integration](../keda.md).

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `keda.enabled` | boolean | Renders KEDA resources in managed mode. | No | `false` |
| `keda.pollingInterval` | integer | KEDA polling interval in seconds. | No | `30` |
| `keda.cooldownPeriod` | integer | KEDA cooldown period in seconds. | No | `300` |
| `keda.minReplicaCount` | integer | Lower bound written through the KEDA `/scale` path. | No | `1` |
| `keda.maxReplicaCount` | integer | Upper bound written through the KEDA `/scale` path. | No | `3` |
| `keda.triggers[]` | object list | KEDA trigger definitions. | No | `[]` |

## Nested App Chart Values

`nifi.*` is the full nested `charts/nifi` configuration surface.

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `nifi.replicaCount` | integer | NiFi pod count for the nested app chart. | No | chart-derived |
| `nifi.image.*` | object | NiFi image repository, tag, and pull policy. | No | chart-derived |
| `nifi.tls.*` | object | TLS source, Secret keys, and cert-manager integration. | No | chart-derived |
| `nifi.auth.*` | object | NiFi authentication provider settings. | No | chart-derived |
| `nifi.authz.*` | object | NiFi authorization bootstrap and policy settings. | No | chart-derived |
| `nifi.ingress.*` | object | Standard Kubernetes ingress settings. | No | chart-derived |
| `nifi.openshift.route.*` | object | OpenShift Route settings. | No | chart-derived |
| `nifi.observability.metrics.*` | object | Metrics subsystem settings. | No | chart-derived |
| `nifi.persistence.*` | object | Repository storage settings. | No | chart-derived |
| `nifi.resources.*` | object | NiFi pod resources. | No | chart-derived |

Use [App Chart Values Reference](nifi-values.md) for the detailed app-chart field map.
