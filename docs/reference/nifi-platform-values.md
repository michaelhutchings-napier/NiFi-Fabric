# Platform Chart Values Reference

This page summarizes the customer-facing configuration surface of `charts/nifi-platform`.

File of record:

- `charts/nifi-platform/values.yaml`

For install steps, see [Install with Helm](../install/helm.md). For managed lifecycle behavior, see [Hibernation and Restore](../manage/hibernation-and-restore.md), [Autoscaling](../manage/autoscaling.md), and [Observability and Metrics](../manage/observability-metrics.md).

## Install Mode

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `mode` | enum | Install mode. Values: `standalone`, `managed`, `managed-cert-manager`. | No | `standalone` |

## Controller Settings

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `controller.namespace.name` | string | Namespace for the controller Deployment. | No | `nifi-system` |
| `controller.namespace.create` | boolean | Creates the controller namespace when `true`. | No | `true` |
| `controller.serviceAccount.name` | string | Existing ServiceAccount name to reuse. Leave empty for the chart-managed default. | No | `""` |
| `controller.image.repository` | string | Controller image repository. | No | `nifi-fabric-controller` |
| `controller.image.tag` | string | Controller image tag. | No | `dev` |
| `controller.image.pullPolicy` | string | Controller image pull policy. | No | `IfNotPresent` |
| `controller.imagePullSecrets[]` | object list | Image pull secrets for the controller Deployment. | No | `[]` |
| `controller.automountServiceAccountToken` | boolean | Automounts the ServiceAccount token into the controller pod. This defaults to `true` because the controller uses in-cluster Kubernetes client configuration. | No | `true` |
| `controller.enableServiceLinks` | boolean | Enables Kubernetes service environment variable injection into the controller pod. | No | `false` |
| `controller.podSecurityContext.fsGroup` | integer | Pod file-system group for the controller pod. | No | `65532` |
| `controller.securityContext.runAsUser` | integer | Controller container user ID. | No | `65532` |
| `controller.securityContext.runAsGroup` | integer | Controller container group ID. | No | `65532` |
| `controller.securityContext.runAsNonRoot` | boolean | Requires non-root execution for the controller container. | No | `true` |
| `controller.securityContext.allowPrivilegeEscalation` | boolean | Disables privilege escalation for the controller container by default. | No | `false` |
| `controller.securityContext.capabilities.drop[]` | string list | Linux capabilities dropped by default for the controller container. | No | `["ALL"]` |
| `controller.securityContext.seccompProfile.type` | string | Default seccomp profile type for the controller container. | No | `RuntimeDefault` |
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
| `cluster.autoscaling.scaleDown.enabled` | boolean | Enables controller-owned safe scale-down for the bounded execution path. | No | `false` |
| `cluster.autoscaling.scaleDown.cooldown` | duration | Minimum time between scale-down actions. | No | `10m` |
| `cluster.autoscaling.scaleDown.stabilizationWindow` | duration | Required low-pressure stability before scale-down. | No | `5m` |
| `cluster.autoscaling.scaleDown.maxSequentialSteps` | integer | Maximum number of one-node removals the controller may complete in one bounded sequential scale-down episode. | No | `1` |
| `cluster.autoscaling.external.enabled` | boolean | Enables the external intent surface used by optional KEDA integration. | No | `false` |
| `cluster.autoscaling.external.source` | string | External source name. Current supported value is `KEDA`. | No | `""` |
| `cluster.autoscaling.external.scaleDownEnabled` | boolean | Allows best-effort external downscale intent to be considered through the existing bounded controller-owned scale-down path. | No | `false` |
| `cluster.autoscaling.external.requestedReplicas` | integer | External requested replicas observed through `/scale`. | No | `0` |
| `cluster.autoscaling.minReplicas` | integer | Lower autoscaling bound. | No | `0` |
| `cluster.autoscaling.maxReplicas` | integer | Upper autoscaling bound. | No | `0` |
| `cluster.autoscaling.signals[]` | string list | Signals the controller may evaluate. | No | `[]` |

## Optional trust-manager Integration

`trustManager.*` is optional. It renders a trust-manager `Bundle` for the NiFi release namespace. Source objects can be operator-provided in trust-manager's configured trust namespace, or the platform chart can mirror the workload TLS `ca.crt` into a trust-manager source Secret automatically.

The same bundle can be consumed by:

- `nifi.tls.additionalTrustBundle.*`
- `nifi.observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle=true`
- `nifi.observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle=true`

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `trustManager.enabled` | boolean | Enables trust-manager `Bundle` rendering in managed modes. | No | `false` |
| `trustManager.sources.useDefaultCAs` | boolean | Includes trust-manager's default CA bundle source. | No | `false` |
| `trustManager.sources.configMaps[]` | object list | Source ConfigMaps in the trust namespace. | No | `[]` |
| `trustManager.sources.secrets[]` | object list | Source Secrets in the trust namespace. | No | `[]` |
| `trustManager.sources.inline[]` | object list | Inline PEM bundle sources. | No | `[]` |
| `trustManager.target.type` | enum | Bundle target type. Values: `configMap`, `secret`. Secret targets require upstream trust-manager secret-target support. | No | `configMap` |
| `trustManager.target.key` | string | Key written into the target ConfigMap or Secret. | No | `ca.crt` |
| `trustManager.target.additionalFormats.pkcs12.enabled` | boolean | Renders an extra PKCS12 output in the Bundle target when supported by trust-manager. | No | `false` |
| `trustManager.target.additionalFormats.pkcs12.key` | string | Key used for the PKCS12 output. | No | `truststore.p12` |
| `trustManager.target.additionalFormats.pkcs12.password` | string | Optional password written into the PKCS12 output. | No | `""` |
| `trustManager.target.additionalFormats.pkcs12.profile` | enum | Optional PKCS12 compatibility profile. Values: `LegacyRC2`, `LegacyDES`, `Modern2023`. | No | `""` |
| `trustManager.target.additionalFormats.jks.enabled` | boolean | Renders an extra JKS output in the Bundle target when supported by trust-manager. | No | `false` |
| `trustManager.target.additionalFormats.jks.key` | string | Key used for the JKS output. | No | `truststore.jks` |
| `trustManager.target.additionalFormats.jks.password` | string | Password written into the JKS output. | No | `changeit` |
| `trustManager.target.labels` | object | Extra labels for the target Bundle metadata. | No | `{}` |
| `trustManager.target.annotations` | object | Extra annotations for the target Bundle metadata. | No | `{}` |
| `trustManager.mirrorTLSSecret.enabled` | boolean | Mirrors the workload TLS `ca.crt` into trust-manager's trust namespace using a chart-owned helper Job and CronJob. | No | `false` |
| `trustManager.mirrorTLSSecret.trustNamespace` | string | Namespace where trust-manager reads mirrored source Secrets. | No | `cert-manager` |
| `trustManager.mirrorTLSSecret.sourceSecretName` | string | Workload TLS Secret name to mirror. Leave empty to reuse the configured NiFi TLS Secret name. | No | `""` |
| `trustManager.mirrorTLSSecret.sourceKey` | string | Key read from the workload TLS Secret. | No | `ca.crt` |
| `trustManager.mirrorTLSSecret.targetSecretName` | string | Secret name written into the trust namespace. Leave empty for the chart-generated default. | No | `""` |
| `trustManager.mirrorTLSSecret.targetKey` | string | Key written into the mirrored trust-manager source Secret. | No | `ca.crt` |
| `trustManager.mirrorTLSSecret.schedule` | string | Cron schedule for recurring source synchronization. | No | `*/1 * * * *` |
| `trustManager.mirrorTLSSecret.image.repository` | string | Mirror helper image repository. | No | `bitnami/kubectl` |
| `trustManager.mirrorTLSSecret.image.tag` | string | Optional mirror helper image tag. Leave empty when using a digest-pinned image reference. | No | `""` |
| `trustManager.mirrorTLSSecret.image.digest` | string | Mirror helper image digest used by the default reproducible path. | No | `sha256:6e2cdb22d6ab7264ea198c717f555e30536b54029d26c8781b9f25f78951b564` |
| `trustManager.mirrorTLSSecret.image.pullPolicy` | string | Mirror helper image pull policy. | No | `IfNotPresent` |
| `trustManager.mirrorTLSSecret.resources.*` | object | Mirror helper Job and CronJob resources. | No | see values file |

## Optional KEDA Integration

`keda.*` is optional and supported as an external intent input path. For behavior details, see [KEDA integration](../keda.md).

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
| `nifi.imagePullSecrets[]` | object list | Image pull secrets for chart-managed nested NiFi pods. | No | chart-derived |
| `nifi.automountServiceAccountToken` | boolean | Automounts the ServiceAccount token into chart-managed nested NiFi pods. | No | chart-derived |
| `nifi.enableServiceLinks` | boolean | Enables Kubernetes service environment variable injection into chart-managed nested NiFi pods. | No | chart-derived |
| `nifi.linkerd.*` | object | Optional bounded Linkerd compatibility settings for the nested NiFi workload only. | No | chart-derived |
| `nifi.istio.*` | object | Optional bounded Istio sidecar-mode compatibility settings for the nested NiFi workload only. | No | chart-derived |
| `nifi.ambient.*` | object | Optional bounded Istio Ambient compatibility settings for the nested NiFi workload only. | No | chart-derived |
| `nifi.tls.*` | object | TLS source, Secret keys, cert-manager integration, and optional extra trust bundle import. | No | chart-derived |
| `nifi.auth.*` | object | NiFi authentication provider settings. | No | chart-derived |
| `nifi.authz.*` | object | NiFi authorization bootstrap, bounded mutable-flow capability bundles, and policy settings. | No | chart-derived |
| `nifi.ingress.*` | object | Standard Kubernetes ingress settings. | No | chart-derived |
| `nifi.openshift.route.*` | object | OpenShift Route settings. | No | chart-derived |
| `nifi.observability.metrics.*` | object | Metrics subsystem settings, including optional trust-manager bundle consumption. | No | chart-derived |
| `nifi.persistence.*` | object | Repository storage settings. | No | chart-derived |
| `nifi.resources.*` | object | NiFi pod resources. | No | chart-derived |
| `nifi.env[]` | object list | Extra environment variables appended to the main nested NiFi container. | No | chart-derived |
| `nifi.envFrom[]` | object list | Extra environment sources appended to the main nested NiFi container. | No | chart-derived |
| `nifi.extraVolumes[]` | object list | Extra pod volumes appended to the nested NiFi pod. | No | chart-derived |
| `nifi.extraVolumeMounts[]` | object list | Extra volume mounts appended to the main nested NiFi container. | No | chart-derived |
| `nifi.podLabels` | object | Additional labels attached to the nested NiFi pod template. | No | chart-derived |
| `nifi.podAnnotations` | object | Additional annotations attached to the nested NiFi pod template. | No | chart-derived |
| `nifi.hostAliases[]` | object list | Pod host aliases for the nested NiFi workload. | No | chart-derived |
| `nifi.priorityClassName` | string | Pod priority class name for the nested NiFi workload. | No | chart-derived |
| `nifi.terminationGracePeriodSeconds` | integer | NiFi pod termination grace period before force-kill. | No | chart-derived |
| `nifi.extraInitContainers[]` | object list | Extra raw Kubernetes init containers for the nested NiFi pod. | No | chart-derived |
| `nifi.extraInitContainersSecurityContext.*` | object | Default security context merged into nested extra init containers. | No | chart-derived |
| `nifi.sidecars[]` | object list | Extra raw Kubernetes sidecar containers for the nested NiFi pod. | No | chart-derived |
| `nifi.sidecarsSecurityContext.*` | object | Default security context merged into nested sidecars. | No | chart-derived |
| `nifi.securityContext.*` | object | Base container security context for chart-managed nested NiFi containers, including the default non-root, no-privilege-escalation, drop-all-capabilities, and `RuntimeDefault` seccomp posture. | No | chart-derived |

Use [App Chart Values Reference](app-chart-values.md) for the detailed app-chart field map.
