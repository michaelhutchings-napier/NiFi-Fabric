# App Chart Values Reference

This page summarizes the customer-facing configuration surface of `charts/nifi`.

File of record:

- `charts/nifi/values.yaml`

For install guidance, see [Install with Helm](../install/helm.md). For feature behavior, use the manage pages such as [TLS and cert-manager](../manage/tls-and-cert-manager.md), [Authentication](../manage/authentication.md), [Observability and Metrics](../manage/observability.md), and [Flow Registry Clients](../manage/flow-registry-clients.md).

## Workload Shape and Version

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `replicaCount` | integer | Number of NiFi pods. | No | `3` |
| `image.repository` | string | NiFi image repository. | No | `apache/nifi` |
| `image.tag` | string | NiFi image tag. | No | `2.0.0` |
| `image.pullPolicy` | string | NiFi image pull policy. | No | `IfNotPresent` |
| `controllerManaged.enabled` | boolean | Enables the managed `OnDelete` workload shape used by the controller path. | No | `false` |

## Identity, Service, and Exposure

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `serviceAccount.create` | boolean | Creates a workload ServiceAccount. | No | `true` |
| `serviceAccount.name` | string | Existing ServiceAccount name to reuse. | No | `""` |
| `service.type` | string | Main NiFi Service type. | No | `ClusterIP` |
| `service.annotations` | object | Annotations for the main Service. | No | `{}` |
| `service.sessionAffinity` | string | Service session affinity mode. | No | `None` |
| `ports.https` | integer | HTTPS port. | No | `8443` |
| `ports.cluster` | integer | NiFi cluster protocol port. | No | `11443` |
| `ports.loadBalance` | integer | NiFi load-balance port. | No | `6342` |
| `web.proxyHosts[]` | string list | Proxy hosts passed to NiFi. | No | `[]` |
| `ingress.enabled` | boolean | Enables Kubernetes Ingress rendering. | No | `false` |
| `ingress.className` | string | Ingress class name. | No | `""` |
| `ingress.annotations` | object | Ingress annotations. | No | `{}` |
| `ingress.hosts[]` | object list | Ingress host and path rules. | No | see values file |
| `ingress.tls[]` | object list | Ingress TLS entries. | No | `[]` |
| `openshift.route.enabled` | boolean | Enables OpenShift Route rendering. | No | `false` |
| `openshift.route.host` | string | Explicit Route host. | No | `""` |
| `openshift.route.annotations` | object | Route annotations. | No | `{}` |
| `openshift.route.tls.termination` | string | Route TLS termination mode. | No | `passthrough` |
| `openshift.route.tls.insecureEdgeTerminationPolicy` | string | Route insecure edge policy. | No | `None` |

## TLS and Certificates

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `tls.mode` | enum | TLS source. Values: `externalSecret`, `certManager`. | No | `externalSecret` |
| `tls.existingSecret` | string | Existing TLS Secret name for external Secret mode. | No | `nifi-tls` |
| `tls.mountPath` | string | TLS mount path inside the NiFi container. | No | `/opt/nifi/tls` |
| `tls.caCertKey` | string | CA certificate key in the TLS Secret. | No | `ca.crt` |
| `tls.keystoreKey` | string | PKCS12 keystore key in the TLS Secret. | No | `keystore.p12` |
| `tls.truststoreKey` | string | PKCS12 truststore key in the TLS Secret. | No | `truststore.p12` |
| `tls.keystorePasswordKey` | string | Keystore password key in the TLS Secret. | No | `keystorePassword` |
| `tls.truststorePasswordKey` | string | Truststore password key in the TLS Secret. | No | `truststorePassword` |
| `tls.sensitivePropsKeyKey` | string | Sensitive properties key key in the TLS Secret. | No | `sensitivePropsKey` |
| `tls.sensitiveProps.value` | string | Inline sensitive properties key. | No | `""` |
| `tls.sensitiveProps.secretRef.*` | object | Secret reference for the sensitive properties key. | No | empty |
| `tls.autoReload.enabled` | boolean | Enables NiFi TLS autoreload. | No | `false` |
| `tls.autoReload.interval` | string | NiFi TLS autoreload interval. | No | `10 secs` |
| `tls.certManager.enabled` | boolean | Enables cert-manager resource rendering. | No | `false` |
| `tls.certManager.issuerRef.*` | object | cert-manager issuer reference. | No | see values file |
| `tls.certManager.secretName` | string | Target Secret name written by cert-manager. | No | `nifi-tls` |
| `tls.certManager.duration` | string | Certificate duration. | No | `2160h` |
| `tls.certManager.renewBefore` | string | Certificate renewal window. | No | `360h` |
| `tls.certManager.commonName` | string | Certificate common name override. | No | `""` |
| `tls.certManager.dnsNames[]` | string list | Additional certificate DNS names. | No | `[]` |
| `tls.certManager.usages[]` | string list | Certificate usages. | No | `["server auth","client auth"]` |
| `tls.certManager.privateKey.*` | object | Private key settings. | No | see values file |
| `tls.certManager.pkcs12.*` | object | PKCS12 output settings. | No | see values file |

## Authentication and Authorization

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `auth.mode` | enum | Authentication mode. Values: `singleUser`, `oidc`, `ldap`. | No | `singleUser` |
| `auth.singleUser.existingSecret` | string | Secret holding single-user credentials. | No | `nifi-auth` |
| `auth.singleUser.usernameKey` | string | Username key in the single-user Secret. | No | `username` |
| `auth.singleUser.passwordKey` | string | Password key in the single-user Secret. | No | `password` |
| `auth.oidc.discoveryUrl` | string | OIDC discovery URL. | No | `""` |
| `auth.oidc.clientId` | string | OIDC client ID. | No | `""` |
| `auth.oidc.clientSecret.*` | object | OIDC client Secret reference. | No | empty |
| `auth.oidc.additionalScopes[]` | string list | Additional OIDC scopes. | No | `["openid","profile","email"]` |
| `auth.oidc.claims.identifyingUser` | string | Claim used as the NiFi user identity. | No | `email` |
| `auth.oidc.claims.groups` | string | Claim used for group mapping. | No | `groups` |
| `auth.oidc.extraProperties` | object | Additional NiFi OIDC properties. | No | `{}` |
| `auth.ldap.url` | string | LDAP server URL. | No | `""` |
| `auth.ldap.authenticationStrategy` | string | LDAP authentication strategy. | No | `START_TLS` |
| `auth.ldap.identityStrategy` | string | LDAP identity strategy. | No | `USE_DN` |
| `auth.ldap.managerSecret.*` | object | LDAP manager credential Secret reference. | No | empty |
| `auth.ldap.userSearch.*` | object | LDAP user search settings. | No | see values file |
| `auth.ldap.groupSearch.*` | object | LDAP group search settings. | No | see values file |
| `authz.mode` | string | Authorization mode. | No | `fileManaged` |
| `authz.bootstrap.initialAdminGroup` | string | Initial admin group bootstrap value. | No | `""` |
| `authz.bootstrap.initialAdminIdentity` | string | Initial admin identity bootstrap value. | No | `""` |
| `authz.applicationGroups[]` | object list | Application groups seeded into NiFi. | No | `[]` |
| `authz.policies[]` | object list | File-managed policy definitions. | No | `[]` |

## Observability and Metrics

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `serviceMonitor.enabled` | boolean | Deprecated compatibility shim for older ServiceMonitor-only configuration. | No | `false` |
| `serviceMonitor.interval` | string | Deprecated compatibility shim interval. | No | `30s` |
| `serviceMonitor.scrapeTimeout` | string | Deprecated compatibility shim scrape timeout. | No | `10s` |
| `observability.metrics.mode` | enum | Metrics mode. Values: `disabled`, `nativeApi`, `exporter`, `siteToSite`. | No | `disabled` |
| `observability.metrics.nativeApi.service.*` | object | Native metrics Service settings. | No | see values file |
| `observability.metrics.nativeApi.serviceMonitor.defaults.*` | object | Default ServiceMonitor settings for native metrics endpoints. | No | see values file |
| `observability.metrics.nativeApi.machineAuth.*` | object | Provider-agnostic machine-auth Secret contract. | No | see values file |
| `observability.metrics.nativeApi.tlsConfig.*` | object | TLS settings for secured native metrics scraping. | No | see values file |
| `observability.metrics.nativeApi.endpoints[]` | object list | Named native metrics scrape profiles. | No | see values file |
| `observability.metrics.exporter.image.*` | object | Experimental exporter image settings. | No | see values file |
| `observability.metrics.exporter.service.*` | object | Experimental exporter Service settings. | No | see values file |
| `observability.metrics.exporter.serviceMonitor.*` | object | Experimental exporter ServiceMonitor settings. | No | see values file |
| `observability.metrics.exporter.machineAuth.*` | object | Machine-auth Secret contract for exporter upstream scraping. | No | see values file |
| `observability.metrics.exporter.source.*` | object | Upstream NiFi metrics source settings for the exporter. | No | see values file |
| `observability.metrics.exporter.supplemental.flowStatus.*` | object | Optional controller-status gauges derived from `/nifi-api/flow/status`. | No | see values file |
| `observability.metrics.exporter.resources.*` | object | Exporter pod resources. | No | `{}` |
| `observability.metrics.siteToSite.*` | object | Prepared-only site-to-site metrics contract. | No | see values file |

## Flow Registry Clients

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `flowRegistryClients.enabled` | boolean | Enables prepared Flow Registry Client catalog rendering. | No | `false` |
| `flowRegistryClients.mountPath` | string | Mount path for the rendered catalog files. | No | `/opt/nifi/fabric/flow-registry-clients` |
| `flowRegistryClients.clients[]` | object list | Prepared client definitions. | No | `[]` |

## Availability, Storage, and Resources

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `pdb.enabled` | boolean | Enables the pod disruption budget. | No | `true` |
| `pdb.minAvailable` | integer | Minimum available pods for the pod disruption budget. | No | `1` |
| `persistence.enabled` | boolean | Enables persistent storage for repositories. | No | `true` |
| `persistence.storageClassName` | string | StorageClass name override. | No | `""` |
| `persistence.accessModes[]` | string list | PVC access modes. | No | `["ReadWriteOnce"]` |
| `persistence.databaseRepository.size` | string | Database repository PVC size. | No | `2Gi` |
| `persistence.flowfileRepository.size` | string | FlowFile repository PVC size. | No | `2Gi` |
| `persistence.contentRepository.size` | string | Content repository PVC size. | No | `4Gi` |
| `persistence.provenanceRepository.size` | string | Provenance repository PVC size. | No | `4Gi` |
| `resources.*` | object | NiFi container resource requests and limits. | No | `{}` |

## Scheduling and Security

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `affinity` | object | Pod affinity and anti-affinity rules. | No | `{}` |
| `tolerations[]` | object list | Pod tolerations. | No | `[]` |
| `nodeSelector` | object | Pod node selector. | No | `{}` |
| `topologySpreadConstraints[]` | object list | Pod topology spread constraints. | No | `[]` |
| `podSecurityContext.fsGroup` | integer | Pod file-system group. | No | `1000` |
| `securityContext.runAsUser` | integer | Container user ID. | No | `1000` |
| `securityContext.runAsGroup` | integer | Container group ID. | No | `1000` |
| `securityContext.runAsNonRoot` | boolean | Requires non-root execution. | No | `true` |

## NiFi Runtime and Health Checks

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `nifi.jvmHeapInit` | string | Initial JVM heap. | No | `1g` |
| `nifi.jvmHeapMax` | string | Maximum JVM heap. | No | `1g` |
| `nifi.cluster.*` | object | NiFi cluster runtime properties. | No | see values file |
| `probes.startup.*` | object | Startup probe timings. | No | see values file |
| `probes.readiness.*` | object | Readiness probe timings. | No | see values file |
| `probes.liveness.*` | object | Liveness probe timings. | No | see values file |
| `config.extraProperties` | object | Extra `nifi.properties` entries rendered by the chart. | No | `{}` |

## Metrics Mode Support Summary

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `observability.metrics.mode=nativeApi` | support status | Primary supported metrics mode. | No |  |
| `observability.metrics.mode=exporter` | support status | Experimental mode with live proof for flow Prometheus passthrough and selected `/flow/status` gauges. | No |  |
| `observability.metrics.mode=siteToSite` | support status | Prepared-only contract, not runtime-enabled. | No |  |
