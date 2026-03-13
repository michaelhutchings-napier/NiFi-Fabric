# App Chart Values Reference

This page summarizes the real configuration surface of `charts/nifi`.

File of record:

- `charts/nifi/values.yaml`

## Core Values

| Path | Default | Description |
| --- | --- | --- |
| `replicaCount` | `3` | NiFi replica count for standalone installs or the initial managed chart target. |
| `image.*` | `apache/nifi:2.0.0` | NiFi image repository, tag, and pull policy. |
| `controllerManaged.enabled` | `false` | Enables managed `OnDelete` behavior when used with the controller path. |
| `serviceAccount.*` | see values file | Workload ServiceAccount settings. |
| `service.*` | `ClusterIP` | Main NiFi Service settings. |
| `ports.*` | `8443`, `11443`, `6342` | HTTPS, cluster, and load-balance ports. |

## TLS

| Path | Description |
| --- | --- |
| `tls.mode` | `externalSecret` or `certManager`. |
| `tls.existingSecret` | Secret name used for external TLS mode. |
| `tls.mountPath` | TLS mount location inside the NiFi pod. |
| `tls.*Key` | Secret key names for CA, keystore, truststore, and passwords. |
| `tls.sensitiveProps.*` | Sensitive properties key value or Secret reference. |
| `tls.autoReload.*` | NiFi TLS autoreload settings. |
| `tls.certManager.*` | cert-manager Certificate configuration. |

## Authentication and Authorization

| Path | Description |
| --- | --- |
| `auth.mode` | `singleUser`, `oidc`, or `ldap`. |
| `auth.singleUser.*` | Existing Secret and key names for single-user mode. |
| `auth.oidc.*` | OIDC discovery URL, client ID, secret reference, claims, and extra properties. |
| `auth.ldap.*` | LDAP connection, search, TLS, and manager Secret settings. |
| `authz.mode` | `fileManaged`, `externalClaimGroups`, or `ldapSync` through chart validation rules. |
| `authz.bootstrap.*` | Initial admin bootstrap values. |
| `authz.applicationGroups[]` | Application groups seeded into NiFi. |
| `authz.policies[]` | File-managed policy definitions. |

## Exposure

| Path | Description |
| --- | --- |
| `web.proxyHosts[]` | Explicit proxy host list for NiFi. |
| `ingress.*` | Standard Kubernetes Ingress configuration. |
| `openshift.route.*` | OpenShift Route configuration. |

## Observability and Metrics

| Path | Description |
| --- | --- |
| `observability.metrics.mode` | `disabled`, `nativeApi`, `exporter`, or prepared-only `siteToSite`. |
| `observability.metrics.nativeApi.*` | Primary supported metrics mode. |
| `observability.metrics.exporter.*` | Experimental exporter mode. |
| `observability.metrics.siteToSite.*` | Prepared-only contract for a future site-to-site path. |
| `serviceMonitor.*` | Deprecated compatibility shim. Prefer `observability.metrics`. |

## Flow Registry Clients

| Path | Description |
| --- | --- |
| `flowRegistryClients.enabled` | Enables Flow Registry Client catalog rendering. |
| `flowRegistryClients.mountPath` | Mount path for rendered catalog files. |
| `flowRegistryClients.clients[]` | Prepared client definitions. |

## Persistence, Scheduling, and Runtime

| Path | Description |
| --- | --- |
| `pdb.*` | Pod disruption budget configuration. |
| `persistence.*` | Repository PVC sizes and storage class settings. |
| `resources.*` | NiFi container resources. |
| `affinity`, `tolerations`, `nodeSelector`, `topologySpreadConstraints` | Scheduling controls. |
| `podSecurityContext`, `securityContext` | Pod and container security settings. |
| `nifi.*` | JVM and cluster property overrides. |
| `probes.*` | Startup, readiness, and liveness probe settings. |
| `config.extraProperties` | Extra `nifi.properties` values rendered by the chart. |

## Metrics Mode Support Summary

| Mode | Status | Notes |
| --- | --- | --- |
| `nativeApi` | Primary supported | Chart-managed secured API scraping. |
| `exporter` | Experimental | Companion exporter, flow metrics only. |
| `siteToSite` | Prepared-only | Contract only, not runtime-enabled. |
