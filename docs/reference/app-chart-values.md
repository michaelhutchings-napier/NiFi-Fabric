# App Chart Values Reference

This page summarizes the customer-facing configuration surface of `charts/nifi`.

File of record:

- `charts/nifi/values.yaml`

For install guidance, see [Install with Helm](../install/helm.md). For feature behavior, use the manage pages such as [TLS and cert-manager](../manage/tls-and-cert-manager.md), [Authentication](../manage/authentication.md), [Observability and Metrics](../manage/observability-metrics.md), and [Flow Registry Clients](../manage/flow-registry-clients.md).

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
| `tls.additionalTrustBundle.enabled` | boolean | Enables import of an extra PEM CA bundle into a writable copy of the NiFi truststore. | No | `false` |
| `tls.additionalTrustBundle.useTrustManagerBundle` | boolean | Uses the configured `trustManagerBundleRef.*` Bundle source from the optional platform trust-manager integration. | No | `false` |
| `tls.additionalTrustBundle.mountPath` | string | Mount path for the extra CA bundle inside the init container. | No | `/opt/nifi/trust-bundle` |
| `tls.additionalTrustBundle.configMapRef.*` | object | ConfigMap reference for an extra PEM CA bundle. | No | empty |
| `tls.additionalTrustBundle.secretRef.*` | object | Secret reference for an extra PEM CA bundle. | No | empty |
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

## Shared Trust Bundle Reference

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `trustManagerBundleRef.type` | enum | Bundle source type used when app consumers enable `useTrustManagerBundle`. Values: `configMap`, `secret`. | No | `configMap` |
| `trustManagerBundleRef.name` | string | Explicit Bundle source name override. Leave empty to use the platform-generated `<release>-nifi-trust-bundle` name. | No | `""` |
| `trustManagerBundleRef.key` | string | Bundle key consumed by app-level trust-manager integrations. | No | `ca.crt` |

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
| `authz.bundles.viewer.*` | object | Named read-only bundle binding for viewer groups. | No | see values file |
| `authz.bundles.editor.*` | object | Named process-group editing bundle binding. | No | see values file |
| `authz.bundles.flowVersionManager.*` | object | Named version-control workflow bundle binding. | No | see values file |
| `authz.bundles.admin.*` | object | Named admin bundle binding. | No | see values file |
| `authz.capabilities.mutableFlow.enabled` | boolean | Enables the bounded mutable-flow capability bundle for inherited root-canvas process-group write and process-group-level version-control access. | No | `false` |
| `authz.capabilities.mutableFlow.includeInitialAdmin` | boolean | Also grants the mutable-flow bundle to the bootstrap admin path. | No | `true` |
| `authz.capabilities.mutableFlow.groups[]` | string list | Seeded NiFi groups that should receive the mutable-flow bundle. Each group must also be seeded through `authz.applicationGroups[]` or `authz.bootstrap.initialAdminGroup`. | No | `[]` |
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
| `observability.metrics.nativeApi.tlsConfig.*` | object | TLS settings for secured native metrics scraping, including Secret or ConfigMap CA references. | No | see values file |
| `observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle` | boolean | Uses the configured `trustManagerBundleRef.*` Bundle source for native metrics TLS trust. | No | `false` |
| `observability.metrics.nativeApi.tlsConfig.ca.configMapRef.*` | object | ConfigMap CA reference for native metrics TLS. | No | empty |
| `observability.metrics.nativeApi.tlsConfig.ca.secretRef.*` | object | Secret CA reference for native metrics TLS. | No | empty |
| `observability.metrics.nativeApi.endpoints[]` | object list | Named native metrics scrape profiles. | No | see values file |
| `observability.metrics.exporter.image.*` | object | Exporter image settings for the optional experimental secondary metrics path. | No | see values file |
| `observability.metrics.exporter.service.*` | object | Exporter Service settings. | No | see values file |
| `observability.metrics.exporter.serviceMonitor.*` | object | Exporter ServiceMonitor settings. | No | see values file |
| `observability.metrics.exporter.machineAuth.*` | object | Machine-auth Secret contract for exporter upstream scraping. | No | see values file |
| `observability.metrics.exporter.source.*` | object | Upstream NiFi metrics source settings for the exporter, including Secret or ConfigMap CA references. | No | see values file |
| `observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle` | boolean | Uses the configured `trustManagerBundleRef.*` Bundle source for exporter upstream TLS trust. This is the path exercised by the focused trust-manager exporter runtime proof. | No | `false` |
| `observability.metrics.exporter.source.tlsConfig.ca.configMapRef.*` | object | ConfigMap CA reference for exporter upstream TLS. | No | empty |
| `observability.metrics.exporter.source.tlsConfig.ca.secretRef.*` | object | Secret CA reference for exporter upstream TLS. | No | empty |
| `observability.metrics.exporter.supplemental.flowStatus.*` | object | Optional controller-status gauges derived from `/nifi-api/flow/status`. | No | see values file |
| `observability.metrics.exporter.resources.*` | object | Exporter pod resources. | No | `{}` |
| `observability.metrics.siteToSite.enabled` | boolean | Enables the typed Site-to-Site metrics export path when `observability.metrics.mode=siteToSite`. | No | `false` |
| `observability.metrics.siteToSite.destination.*` | object | Typed destination URL and input-port contract for Site-to-Site metrics export. | No | see values file |
| `observability.metrics.siteToSite.auth.*` | object | Typed Site-to-Site auth contract. Values: `none`, `workloadTLS`, `secretRef`, plus the explicit secure receiver-authorized identity and any referenced Secret material keys. | No | see values file |
| `observability.metrics.siteToSite.source.*` | object | Reporting-task source identity hints. | No | see values file |
| `observability.metrics.siteToSite.transport.*` | object | Site-to-Site transport settings for the typed metrics-export path. | No | see values file |
| `observability.metrics.siteToSite.format.*` | object | Site-to-Site output format settings. Current runtime support is bounded to `AmbariFormat`. | No | see values file |
| `observability.siteToSiteStatus.enabled` | boolean | Enables the typed Site-to-Site status export path. This feature is independent of `observability.metrics.mode`. | No | `false` |
| `observability.siteToSiteStatus.destination.*` | object | Typed destination URL and input-port contract for Site-to-Site status export. | No | see values file |
| `observability.siteToSiteStatus.auth.*` | object | Typed Site-to-Site auth contract for status export. Values: `none`, `workloadTLS`, `secretRef`, plus the explicit secure receiver-authorized identity and any referenced Secret material keys. | No | see values file |
| `observability.siteToSiteStatus.source.*` | object | Optional status-export source URL override. | No | see values file |
| `observability.siteToSiteStatus.transport.*` | object | Site-to-Site transport settings for the typed status-export path. | No | see values file |

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
| `observability.metrics.mode=nativeApi` | support status | Primary production-ready metrics mode. | No |  |
| `observability.metrics.mode=exporter` | support status | Optional experimental secondary mode with focused runtime proof for render/deploy, secured upstream reachability, Prometheus scraping of `/metrics`, selected `/flow/status` gauges, and auth Secret rotation without exporter pod restart. | No |  |
| `observability.metrics.mode=siteToSite` | support status | Optional experimental typed Site-to-Site metrics-export path. Runtime support is bounded to one `SiteToSiteMetricsReportingTask`, one `StandardRestrictedSSLContextService` when secure transport is used, `AmbariFormat`, and the current single-user bootstrap path. It is not a generic NiFi runtime-object framework. | No |  |
| `observability.siteToSiteStatus.enabled=true` | support status | Optional experimental typed Site-to-Site status-export path. Runtime support is bounded to one `SiteToSiteStatusReportingTask`, one `StandardRestrictedSSLContextService` when secure transport is used, fixed JSON status payload defaults, and the current single-user bootstrap path. It is not a generic NiFi runtime-object framework. | No |  |
