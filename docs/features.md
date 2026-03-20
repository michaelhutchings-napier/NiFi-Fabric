# Features

NiFi-Fabric focuses on a small, practical feature set for running Apache NiFi 2.x on Kubernetes.

## Platform Install

- `charts/nifi-platform` is the standard product install path
- one Helm release installs the controller, the NiFi workload, and the managed `NiFiCluster`
- Helm stays in charge of standard Kubernetes resources

## Lifecycle Management

- safe rollout with health checks
- TLS-aware restart handling
- hibernation and restore
- clear status and event reporting for managed lifecycle work

## Security and Access

- cert-manager-first standard install path
- external TLS Secret ownership for advanced installs
- single-user authentication for the standard bootstrap path
- OIDC and LDAP for advanced managed installs
- named authorization bundles for common access levels

## Autoscaling

- controller-owned autoscaling
- advisory mode for recommendations
- enforced mode for bounded scale-up and safe scale-down
- optional KEDA integration through `NiFiCluster`, not direct `StatefulSet` ownership

## Observability

- native NiFi 2 Prometheus metrics as the primary path, with direct secured API scraping and no reverse-proxy sidecar
- optional exporter metrics path
- starter dashboards, alerts, and runbooks
- optional trust-manager integration for shared CA bundle distribution

## Registry and Flow Configuration

- Flow Registry Client catalog support
- bounded versioned-flow import
- bounded Parameter Context management

## Product Direction

- NiFi 2.x only
- Helm-first install model
- thin controller for lifecycle and safety work
- simpler product surface than a broad NiFi operator stack

For install guidance, see [Install with Helm](install/helm.md).
For support and compatibility detail, see [Compatibility](compatibility.md) and [Verification and Support Levels](testing.md).
