# Features

NiFi-Fabric keeps the product surface small and explicit.

## Platform Install

- `charts/nifi-platform` is the standard install path
- one Helm release installs the CRD, controller, RBAC, app chart, and `NiFiCluster`
- `charts/nifi` stays available for standalone or advanced assembly

## Thin Controller Model

- Helm owns the standard Kubernetes resources around NiFi
- the controller owns lifecycle and safety decisions Helm cannot coordinate safely
- NiFi stays responsible for NiFi-native runtime behavior

## Lifecycle Management

- managed rollout with health gates
- TLS restart policy with cert drift observation
- hibernation to zero
- restore to the previous running size
- explicit status and event surfaces for lifecycle state

## Autoscaling

- built-in controller-owned autoscaler is the primary autoscaling model
- `Advisory` mode provides recommendation-only guidance
- `Enforced` mode supports controller-owned scale-up
- one-step safe scale-down is available and intentionally conservative
- direct autoscaler ownership of the NiFi `StatefulSet` is not the supported architecture

## Optional KEDA Integration

- KEDA is optional and experimental
- KEDA targets `NiFiCluster`, not the NiFi `StatefulSet`
- the controller remains the only executor of actual scale actions

## TLS and cert-manager

- external Secret TLS is supported
- cert-manager integration is supported when cert-manager already exists in the cluster
- cert-manager remains a prerequisite, not a bundled dependency

## Authentication

- single-user mode for simple deployments
- OIDC for managed browser-facing identity
- LDAP for enterprise directory integration
- OIDC and LDAP are first-class managed auth options

## Flow Registry Clients

- Git-based Flow Registry Clients are the supported modern direction
- GitHub and GitLab paths have focused runtime proof
- Bitbucket and Azure DevOps definitions are prepared and render-validated

## Observability

- native API metrics are the primary supported metrics mode
- exporter metrics mode is experimental
- site-to-site metrics are prepared-only
- machine-auth metrics credentials use a provider-agnostic Secret contract

## Environment Scope

- kind is the current runtime proof baseline in this repository
- AKS is the primary target environment
- OpenShift is supported as a prepared secondary target
- environment-specific claims remain conservative until runtime proof is recorded
