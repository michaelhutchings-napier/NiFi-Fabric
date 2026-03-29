# App Chart Values Reference

This page lists the main values in `charts/nifi`.

See also:

- [Install with Helm](../install/helm.md)
- [Advanced Install Paths](../install/advanced.md)
- [TLS and cert-manager](../manage/tls-and-cert-manager.md)
- [Authentication](../manage/authentication.md)
- [Observability and Metrics](../manage/observability-metrics.md)

## Workload Shape and Version

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `replicaCount` | integer | Number of NiFi pods. | No | `3` |
| `image.repository` | string | NiFi image repository. | No | `apache/nifi` |
| `image.tag` | string | NiFi image tag. | No | `2.0.0` |
| `image.pullPolicy` | string | NiFi image pull policy. | No | `IfNotPresent` |
| `imagePullSecrets[]` | object list | Optional image pull secrets for chart-managed pods. | No | `[]` |
| `automountServiceAccountToken` | boolean | Automounts the ServiceAccount token into chart-managed pods. This defaults to `true` because clustered NiFi uses the Kubernetes API for leader-election coordination. | No | `true` |
| `enableServiceLinks` | boolean | Enables Kubernetes service environment variable injection into chart-managed pods. | No | `false` |
| `controllerManaged.enabled` | boolean | Enables the managed `OnDelete` workload shape used by the controller path. | No | `false` |

## Identity, Service, and Exposure

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `serviceAccount.create` | boolean | Creates a workload ServiceAccount. | No | `true` |
| `serviceAccount.name` | string | Existing ServiceAccount name to reuse. | No | `""` |
| `service.type` | string | Main NiFi Service type. | No | `ClusterIP` |
| `service.annotations` | object | Annotations for the main Service. | No | `{}` |
| `service.sessionAffinity` | string | Service session affinity mode. | No | `None` |
| `linkerd.enabled` | boolean | Enables the Linkerd compatibility profile for the NiFi workload. | No | `false` |
| `linkerd.inject` | string | Linkerd injection annotation value applied to the NiFi pod template when Linkerd compatibility is enabled. | No | `enabled` |
| `linkerd.opaquePorts.cluster` | boolean | Marks the NiFi cluster protocol port as opaque for the Linkerd profile. | No | `true` |
| `linkerd.opaquePorts.loadBalance` | boolean | Marks the NiFi load-balance port as opaque for the Linkerd profile. | No | `true` |
| `linkerd.opaquePorts.https` | boolean | Optionally marks the NiFi HTTPS port as opaque too. Leave this `false` for the documented baseline profile unless you have a specific operator reason. | No | `false` |
| `linkerd.opaquePorts.additional[]` | integer list | Additional opaque ports appended to the Linkerd profile. | No | `[]` |
| `istio.enabled` | boolean | Enables the Istio sidecar-mode compatibility profile for the NiFi workload. | No | `false` |
| `istio.inject` | boolean | Applies the Istio sidecar injection annotation to the NiFi pod template when the profile is enabled. The supported profile still expects the NiFi namespace to be injection-enabled by the operator. | No | `true` |
| `istio.rewriteAppHTTPProbers` | boolean | Explicitly enables Istio sidecar probe rewrite for the supported profile so the existing Kubernetes probes remain compatible under sidecar mode. | No | `true` |
| `istio.holdApplicationUntilProxyStarts` | boolean | Adds the Istio proxy-startup annotation used by the supported profile so NiFi waits for the sidecar to be ready first. | No | `true` |
| `istio.annotations` | object | Additional Istio-specific pod annotations appended only when the Istio profile is enabled. | No | `{}` |
| `ambient.enabled` | boolean | Enables the Istio Ambient compatibility profile for the NiFi workload. | No | `false` |
| `ambient.dataplaneMode` | string | Pod-template dataplane-mode label applied when the Ambient profile is enabled. Leave this at `ambient` for the documented supported profile. | No | `ambient` |
| `ambient.labels` | object | Additional Ambient-specific pod labels appended only when the Ambient profile is enabled. | No | `{}` |
| `ports.https` | integer | HTTPS port. | No | `8443` |
| `ports.cluster` | integer | NiFi cluster protocol port. | No | `11443` |
| `ports.loadBalance` | integer | NiFi load-balance port. | No | `6342` |
| `web.proxyHosts[]` | string list | Proxy hosts passed to NiFi. Include the explicit Route host or `host:443` when `openshift.route.enabled=true`. | No | `[]` |
| `ingress.enabled` | boolean | Enables Kubernetes Ingress rendering. | No | `false` |
| `ingress.className` | string | Ingress class name. | No | `""` |
| `ingress.annotations` | object | Ingress annotations. | No | `{}` |
| `ingress.hosts[]` | object-or-string list | Ingress host and path rules. String entries use the host as shorthand for a single `/` `Prefix` path. | No | see values file |
| `ingress.tls[]` | object list | Ingress TLS entries. | No | `[]` |
| `openshift.route.enabled` | boolean | Enables OpenShift Route rendering. The documented model keeps the Route hostname explicit and mirrors it into `web.proxyHosts`. | No | `false` |
| `openshift.route.host` | string | Explicit Route host. Required when `openshift.route.enabled=true`. | No | `""` |
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
| `auth.oidc.preferredJwsAlgorithm` | string | Preferred JWS algorithm for ID token verification. Leave empty to use the provider default. | No | `""` |
| `auth.oidc.additionalScopes[]` | string list | Additional OIDC scopes. | No | `["openid","profile","email"]` |
| `auth.oidc.claims.identifyingUser` | string | Claim used as the NiFi user identity. | No | `email` |
| `auth.oidc.claims.groups` | string | Claim used for group mapping. | No | `groups` |
| `auth.oidc.extraProperties` | object | Additional NiFi OIDC properties. | No | `{}` |
| `auth.ldap.url` | string | LDAP server URL. | No | `""` |
| `auth.ldap.authenticationStrategy` | string | LDAP authentication strategy. | No | `START_TLS` |
| `auth.ldap.identityStrategy` | string | LDAP identity strategy. | No | `USE_DN` |
| `auth.ldap.authenticationExpiration` | string | LDAP authentication expiration window. | No | `12 hours` |
| `auth.ldap.referralStrategy` | string | LDAP referral strategy. | No | `FOLLOW` |
| `auth.ldap.connectTimeout` | string | LDAP connect timeout. | No | `10 secs` |
| `auth.ldap.readTimeout` | string | LDAP read timeout. | No | `10 secs` |
| `auth.ldap.pageSize` | string | Optional LDAP page size override. Leave empty to use the provider default. | No | `""` |
| `auth.ldap.syncInterval` | string | LDAP sync interval for group and user refresh. | No | `30 mins` |
| `auth.ldap.groupMembershipEnforceCaseSensitivity` | boolean | Preserves case sensitivity when evaluating LDAP group membership. | No | `false` |
| `auth.ldap.managerSecret.*` | object | LDAP manager credential Secret reference. | No | empty |
| `auth.ldap.tls.clientAuth` | string | LDAP TLS client-auth mode. | No | `NONE` |
| `auth.ldap.tls.protocol` | string | LDAP TLS protocol. | No | `TLS` |
| `auth.ldap.tls.shutdownGracefully` | boolean | Enables graceful LDAP TLS shutdown. | No | `false` |
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
| `authz.capabilities.mutableFlow.enabled` | boolean | Enables the mutable-flow capability bundle for inherited root-canvas process-group write and process-group-level version-control access. | No | `false` |
| `authz.capabilities.mutableFlow.includeInitialAdmin` | boolean | Also grants the mutable-flow bundle to the bootstrap admin path. | No | `true` |
| `authz.capabilities.mutableFlow.groups[]` | string list | Seeded NiFi groups that should receive the mutable-flow bundle. Each group must also be seeded through `authz.applicationGroups[]` or `authz.bootstrap.initialAdminGroup`. | No | `[]` |
| `authz.policies[]` | object list | File-managed policy definitions. | No | `[]` |

## Observability

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `serviceMonitor.enabled` | boolean | Deprecated compatibility shim for older ServiceMonitor-only configuration. | No | `false` |
| `serviceMonitor.interval` | string | Deprecated compatibility shim interval. | No | `30s` |
| `serviceMonitor.scrapeTimeout` | string | Deprecated compatibility shim scrape timeout. | No | `10s` |
| `observability.metrics.mode` | enum | Metrics mode. Values: `disabled`, `nativeApi`, `exporter`, `siteToSite`. | No | `disabled` |
| `observability.metrics.nativeApi.service.*` | object | Native metrics Service settings. | No | see values file |
| `observability.metrics.nativeApi.serviceMonitor.enabled` | boolean | Enables native API `ServiceMonitor` rendering. Direct `charts/nifi` installs keep this `true` by default for backward compatibility; set it to `false` when Prometheus Operator is not installed and you only want the native metrics `Service`. | No | `true` |
| `observability.metrics.nativeApi.serviceMonitor.defaults.*` | object | Default ServiceMonitor settings for native metrics endpoints. | No | see values file |
| `observability.metrics.nativeApi.machineAuth.*` | object | Provider-agnostic machine-auth Secret settings. | No | see values file |
| `observability.metrics.nativeApi.tlsConfig.*` | object | TLS settings for secured native metrics scraping, including Secret or ConfigMap CA references. | No | see values file |
| `observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle` | boolean | Uses the configured `trustManagerBundleRef.*` Bundle source for native metrics TLS trust. | No | `false` |
| `observability.metrics.nativeApi.tlsConfig.ca.configMapRef.*` | object | ConfigMap CA reference for native metrics TLS. | No | empty |
| `observability.metrics.nativeApi.tlsConfig.ca.secretRef.*` | object | Secret CA reference for native metrics TLS. | No | empty |
| `observability.metrics.nativeApi.endpoints[]` | object list | Named native metrics scrape profiles. | No | see values file |
| `observability.metrics.exporter.image.*` | object | Exporter image settings for the optional secondary metrics path. | No | see values file |
| `observability.metrics.exporter.service.*` | object | Exporter Service settings. | No | see values file |
| `observability.metrics.exporter.serviceMonitor.*` | object | Exporter ServiceMonitor settings. | No | see values file |
| `observability.metrics.exporter.machineAuth.*` | object | Machine-auth Secret settings for exporter upstream scraping. | No | see values file |
| `observability.metrics.exporter.source.*` | object | Upstream NiFi metrics source settings for the exporter, including Secret or ConfigMap CA references. | No | see values file |
| `observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle` | boolean | Uses the configured `trustManagerBundleRef.*` Bundle source for exporter upstream TLS trust. | No | `false` |
| `observability.metrics.exporter.source.tlsConfig.ca.configMapRef.*` | object | ConfigMap CA reference for exporter upstream TLS. | No | empty |
| `observability.metrics.exporter.source.tlsConfig.ca.secretRef.*` | object | Secret CA reference for exporter upstream TLS. | No | empty |
| `observability.metrics.exporter.supplemental.flowStatus.*` | object | Optional controller-status gauges derived from `/nifi-api/flow/status`. | No | see values file |
| `observability.metrics.exporter.resources.*` | object | Exporter pod resources. | No | `{}` |
| `observability.metrics.siteToSite.enabled` | boolean | Enables the typed Site-to-Site metrics export path when `observability.metrics.mode=siteToSite`. | No | `false` |
| `observability.metrics.siteToSite.destination.*` | object | Typed destination URL and input-port settings for Site-to-Site metrics export. | No | see values file |
| `observability.metrics.siteToSite.auth.*` | object | Typed Site-to-Site auth settings. Values: `none`, `workloadTLS`, `secretRef`, plus the explicit secure receiver-authorized identity and any referenced Secret material keys. | No | see values file |
| `observability.metrics.siteToSite.source.*` | object | Reporting-task source identity hints. | No | see values file |
| `observability.metrics.siteToSite.transport.*` | object | Site-to-Site transport settings for the typed metrics-export path. | No | see values file |
| `observability.metrics.siteToSite.format.*` | object | Site-to-Site output format settings. Current runtime support is `AmbariFormat`. | No | see values file |
| `observability.siteToSiteStatus.enabled` | boolean | Enables the typed Site-to-Site status export path. This feature is independent of `observability.metrics.mode`. | No | `false` |
| `observability.siteToSiteStatus.destination.*` | object | Typed destination URL and input-port settings for Site-to-Site status export. | No | see values file |
| `observability.siteToSiteStatus.auth.*` | object | Typed Site-to-Site auth settings for status export. Values: `none`, `workloadTLS`, `secretRef`, plus the explicit secure receiver-authorized identity and any referenced Secret material keys. | No | see values file |
| `observability.siteToSiteStatus.source.*` | object | Optional status-export source URL override. | No | see values file |
| `observability.siteToSiteStatus.transport.*` | object | Site-to-Site transport settings for the typed status-export path. | No | see values file |
| `observability.siteToSiteProvenance.enabled` | boolean | Enables the typed Site-to-Site provenance export path. This feature is independent of `observability.metrics.mode` and `observability.siteToSiteStatus`. | No | `false` |
| `observability.siteToSiteProvenance.destination.*` | object | Typed destination URL and input-port settings for Site-to-Site provenance export. | No | see values file |
| `observability.siteToSiteProvenance.auth.*` | object | Typed Site-to-Site auth settings for provenance export. Values: `none`, `workloadTLS`, `secretRef`, plus the explicit secure receiver-authorized identity and any referenced Secret material keys. | No | see values file |
| `observability.siteToSiteProvenance.source.*` | object | Optional provenance-export source URL override. | No | see values file |
| `observability.siteToSiteProvenance.transport.*` | object | Site-to-Site transport settings for the typed provenance-export path. | No | see values file |
| `observability.siteToSiteProvenance.provenance.startPosition` | enum | Initial provenance cursor. Values: `beginningOfStream`, `endOfStream`. | No | `endOfStream` |
| `observability.audit.flowActions.enabled` | boolean | Enables the flow-action audit capability. The default supported layer is durable local audit; optional `export.type=log` adds bounded reporter export. Keep this `false` when you do not want the audit feature at all. | No | `false` |
| `observability.audit.flowActions.local.history.enabled` | boolean | Keeps the NiFi-native flow configuration history database explicit. The current supported slice requires this to stay enabled when flow-action audit is enabled. | No | `true` |
| `observability.audit.flowActions.local.archive.enabled` | boolean | Keeps automatic flow archive creation enabled on durable storage. The current supported slice requires this to stay enabled when flow-action audit is enabled. | No | `true` |
| `observability.audit.flowActions.local.archive.directory` | string | Durable archive directory for flow archive files. Use a PVC-backed location, not the default `./conf/archive` path under the chart's `emptyDir`-backed `conf` mount. | No | `/opt/nifi/nifi-current/database_repository/flow-audit-archive` |
| `observability.audit.flowActions.local.archive.retention.maxAge` | string | Maximum flow archive age passed to `nifi.flow.configuration.archive.max.time`. | No | `30 days` |
| `observability.audit.flowActions.local.archive.retention.maxStorage` | string | Maximum flow archive storage passed to `nifi.flow.configuration.archive.max.storage`. | No | `2 GB` |
| `observability.audit.flowActions.local.archive.retention.maxCount` | integer | Maximum number of archive files passed to `nifi.flow.configuration.archive.max.count`. | No | `1000` |
| `observability.audit.flowActions.local.requestLog.enabled` | boolean | Enables explicit request-log format wiring as secondary evidence for the local audit layer. | No | `true` |
| `observability.audit.flowActions.local.requestLog.format` | string | Request-log format written to `nifi.web.request.log.format` when request-log wiring is enabled. | No | see values file |
| `observability.audit.flowActions.export.type` | enum | Export mode for flow-action audit. Values: `disabled`, `log`. `disabled` is the normal default. `log` is an advanced optional path that requires NiFi `image.tag >= 2.4.0` plus the reporter image settings below. | No | `disabled` |
| `observability.audit.flowActions.export.log.installation.image.repository` | string | Reporter image repository used by the chart-managed init container for `export.type=log`. Upstream release plumbing publishes this reporter image to GHCR under `ghcr.io/<owner>/nifi-fabric-flow-action-audit-reporter`, but customers can and should use an internal mirrored registry when public registry access is restricted. | No | `""` |
| `observability.audit.flowActions.export.log.installation.image.tag` | string | Reporter image tag used by the chart-managed init container for `export.type=log`. The release workflow publishes `edge`, `sha-<commit>`, and version tags from `flow-action-audit-reporter-vX.Y.Z`; customers can mirror and pin those tags internally. | No | `""` |
| `observability.audit.flowActions.export.log.installation.image.pullPolicy` | string | Reporter image pull policy used by the chart-managed init container. | No | `IfNotPresent` |
| `observability.audit.flowActions.export.log.installation.image.narPath` | string | Absolute path to the built reporter NAR inside the reporter image. | No | `/opt/nifi-fabric-audit/nifi-flow-action-audit-reporter.nar` |
| `observability.audit.flowActions.content.includeRequestDetails` | boolean | Reserved bounded enrichment switch for the reporter-based export slice. | No | `true` |
| `observability.audit.flowActions.content.includeProcessGroupPath` | boolean | Reserved bounded enrichment switch for the reporter-based export slice. | No | `true` |
| `observability.audit.flowActions.content.propertyValues.mode` | enum | Export redaction mode. The current supported implementation requires `redacted`. | No | `redacted` |
| `observability.audit.flowActions.content.propertyValues.allowlistedProperties[]` | string list | Reserved placeholder for a future explicit allowlist-based mode. The current supported implementation does not use it. | No | `[]` |

## Flow Registry Clients

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `flowRegistryClients.enabled` | boolean | Enables Flow Registry Client catalog rendering. | No | `false` |
| `flowRegistryClients.mountPath` | string | Mount path for the rendered catalog files. | No | `/opt/nifi/fabric/flow-registry-clients` |
| `flowRegistryClients.clients[]` | object list | Client definitions. Supported providers here: `github`, `gitlab`, `bitbucket`, `azureDevOps`, and `nifiRegistry`. | No | `[]` |

## Parameter Contexts

For behavior and examples, see [Parameter Contexts](../manage/parameters.md).

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `parameterContexts.enabled` | boolean | Enables runtime-managed Parameter Context reconciliation. | No | `false` |
| `parameterContexts.mountPath` | string | Mount path for the rendered runtime bundle and status bootstrap files. | No | `/opt/nifi/fabric/parameter-contexts` |
| `parameterContexts.contexts[]` | object list | Declared Parameter Context definitions with inline values, sensitive Secret references, and reference-only provider refs. | No | `[]` |

## Versioned Flow Imports

For behavior and examples, see [Flows](../manage/flows.md).

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `versionedFlowImports.enabled` | boolean | Enables runtime-managed versioned-flow import reconciliation. | No | `false` |
| `versionedFlowImports.mountPath` | string | Mount path for the rendered runtime bundle and status files consumed by the live in-pod reconciler. | No | `/opt/nifi/fabric/versioned-flow-imports` |
| `versionedFlowImports.controllerBridge.enabled` | boolean | Enables the optional `NiFiDataflow` bridge input consumed by the same in-pod reconciler and the matching runtime status ConfigMap observed back into `NiFiDataflow.status`. The chart renders the bridge `ConfigMap`, and the controller updates only the bridge payload. Requires `controllerManaged.enabled=true`. | No | `false` |
| `versionedFlowImports.controllerBridge.mountPath` | string | Mount path for the optional chart-rendered `NiFiDataflow` bridge `ConfigMap`. | No | `/opt/nifi/fabric/nifidataflows` |
| `versionedFlowImports.imports[]` | object list | Declared versioned-flow imports with a selected registry client name, bucket, flow name, selected version identifier or `latest`, one intended root-child target name, and optional direct Parameter Context reference. | No | `[]` |

## Availability, Storage, and Resources

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `pdb.enabled` | boolean | Enables the pod disruption budget. | No | `true` |
| `pdb.minAvailable` | integer | Minimum available pods for the pod disruption budget. | No | `1` |
| `persistence.enabled` | boolean | Enables persistent storage for repositories. | No | `true` |
| `persistence.storageClassName` | string | Shared StorageClass name fallback for repository PVCs. Individual repositories can override it with their own `storageClassName`. | No | `""` |
| `persistence.accessModes[]` | string list | PVC access modes. | No | `["ReadWriteOnce"]` |
| `persistence.databaseRepository.size` | string | Database repository PVC size. | No | `2Gi` |
| `persistence.databaseRepository.storageClassName` | string | Optional database repository StorageClass override. | No | `""` |
| `persistence.flowfileRepository.size` | string | FlowFile repository PVC size. | No | `2Gi` |
| `persistence.flowfileRepository.storageClassName` | string | Optional FlowFile repository StorageClass override. | No | `""` |
| `persistence.contentRepository.size` | string | Content repository PVC size. | No | `4Gi` |
| `persistence.contentRepository.storageClassName` | string | Optional content repository StorageClass override. | No | `""` |
| `persistence.provenanceRepository.size` | string | Provenance repository PVC size. | No | `4Gi` |
| `persistence.provenanceRepository.storageClassName` | string | Optional provenance repository StorageClass override. | No | `""` |
| `resources.*` | object | NiFi container resource requests and limits. | No | `{}` |
| `env[]` | object list | Extra environment variables appended to the main NiFi container. | No | `[]` |
| `envFrom[]` | object list | Extra environment sources appended to the main NiFi container. | No | `[]` |
| `extraVolumes[]` | object list | Extra pod volumes appended to the NiFi pod. | No | `[]` |
| `extraVolumeMounts[]` | object list | Extra volume mounts appended to the main NiFi container. | No | `[]` |

`persistence.storageClassName` remains the simple shared default. When a repository-specific `*.storageClassName` is set, that per-repository value wins for that PVC only. If neither the per-repository field nor the shared fallback is set, Kubernetes uses the cluster default `StorageClass`.

## Scheduling and Security

| Field | Type | Description | Required | Default |
| --- | --- | --- | --- | --- |
| `podLabels` | object | Additional labels attached to the NiFi pod template. | No | `{}` |
| `podAnnotations` | object | Additional annotations attached to the NiFi pod template. | No | `{}` |
| `affinity` | object | Pod affinity and anti-affinity rules. | No | `{}` |
| `tolerations[]` | object list | Pod tolerations. | No | `[]` |
| `nodeSelector` | object | Pod node selector. | No | `{}` |
| `topologySpreadConstraints[]` | object list | Pod topology spread constraints. | No | `[]` |
| `hostAliases[]` | object list | Pod host aliases for the NiFi workload. | No | `[]` |
| `priorityClassName` | string | Pod priority class name for the NiFi workload. | No | `""` |
| `terminationGracePeriodSeconds` | integer | Pod termination grace period for NiFi pods before force-kill. | No | `120` |
| `extraInitContainers[]` | object list | Extra raw Kubernetes init containers appended after the built-in `init-conf` bootstrap container. | No | `[]` |
| `extraInitContainersSecurityContext.*` | object | Default container security context merged into `extraInitContainers[]` entries, on top of the standard NiFi container defaults. | No | `{}` |
| `sidecars[]` | object list | Extra raw Kubernetes sidecar containers appended after the main `nifi` container. | No | `[]` |
| `sidecarsSecurityContext.*` | object | Default container security context merged into `sidecars[]` entries, on top of the standard NiFi container defaults. | No | `{}` |
| `podSecurityContext.fsGroup` | integer | Pod file-system group. | No | `1000` |
| `securityContext.runAsUser` | integer | Container user ID. | No | `1000` |
| `securityContext.runAsGroup` | integer | Container group ID. | No | `1000` |
| `securityContext.runAsNonRoot` | boolean | Requires non-root execution. | No | `true` |
| `securityContext.allowPrivilegeEscalation` | boolean | Disables privilege escalation for chart-managed containers by default. | No | `false` |
| `securityContext.capabilities.drop[]` | string list | Linux capabilities dropped by default for chart-managed containers. | No | `["ALL"]` |
| `securityContext.seccompProfile.type` | string | Default seccomp profile type for chart-managed containers. | No | `RuntimeDefault` |

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
| `config.propertyConfigMaps[]` | object list | Ordered external ConfigMap key references applied after the chart-rendered `nifi.properties` overrides during the `init-conf` bootstrap. Later entries win on duplicate properties. | No | `[]` |
| `config.propertyConfigMapsRestartOnChange` | boolean | In managed platform installs, appends `config.propertyConfigMaps[]` names to the watched ConfigMap restart-trigger set. Standalone installs ignore this flag. | No | `false` |
| `repositoryEncryption.enabled` | boolean | Enables Secret-backed NiFi repository encryption wiring. The chart uses NiFi's generic repository-encryption properties with the `KEYSTORE` provider and protocol version `1`. | No | `false` |
| `repositoryEncryption.mountPath` | string | Read-only mount path for the repository-encryption keystore Secret. | No | `/opt/nifi/repository-encryption` |
| `repositoryEncryption.key.id` | string | NiFi repository-encryption key identifier written into `nifi.properties`. | No | `""` |
| `repositoryEncryption.secretRef.name` | string | Secret name containing the repository-encryption keystore file and password. | No | `""` |
| `repositoryEncryption.secretRef.keystoreKey` | string | Secret key containing the repository-encryption keystore file. | No | `repository.p12` |
| `repositoryEncryption.secretRef.passwordKey` | string | Secret key containing the repository-encryption keystore password. | No | `password` |
| `logging.levels.*` | string map | Optional named logger level overrides patched into NiFi's existing `logback.xml` during `init-conf`. Supported levels are `TRACE`, `DEBUG`, `INFO`, `WARN`, and `ERROR`. | No | `{}` |

### Repository Encryption

`repositoryEncryption.*` is a Secret-backed surface for NiFi's generic repository encryption support. The chart renders protocol version `1`, fixes the provider to `KEYSTORE`, mounts the referenced Secret read-only into the pod, and replaces the keystore password placeholder during the `init-conf` bootstrap.

This surface is intentionally small. It is for the repositories NiFi protects through `nifi.repository.encryption.*`, not a full key-management subsystem. Operators still own the keystore material, Secret lifecycle, and any required key escrow or recovery process.

Repository encryption is not the same thing as PVC or storage-layer encryption. Keep the normal storage controls in place as well. Also note the current support boundary: the chart does not claim coverage for NiFi's database repository through this surface.

Changing `repositoryEncryption.*` updates the chart-managed config and pod spec, so standalone `charts/nifi` installs roll through the existing template checksum and StatefulSet diff. In managed `charts/nifi-platform` installs, the nested secret reference is also appended to the controller restart-trigger set so secret changes can drive a controlled rollout. Disabling or rotating repository encryption on an existing installation is operationally sensitive and should be treated as a planned change, not a casual toggle.

### Logger Level Overrides

`logging.levels.*` is a narrow troubleshooting surface for named loggers such as `org.apache.nifi` or `org.apache.nifi.web.api`. The chart does not take ownership of the full `logback.xml`; it copies the upstream file into the writable config directory during `init-conf`, updates existing `<logger>` entries when they exist, and inserts new named logger entries before the root logger block when they do not.

Changing `logging.levels.*` updates the chart-managed config ConfigMap and rolls standalone `charts/nifi` pods through the existing `checksum/config` annotation. In managed `charts/nifi-platform` installs, the same change flows through the nested app chart and the controller-managed rollout path. If a logging override causes startup trouble, revert the map entry and inspect the `init-conf` container logs for the failing pod.
