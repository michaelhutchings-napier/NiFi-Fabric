# MVP

## Exact MVP Features

The MVP includes:

- a standalone Helm chart for NiFi 2.x
- optional controller-managed mode
- one namespaced CRD: `NiFiCluster`
- secure-by-default TLS assumptions with cert-manager integration
- persistent volumes for NiFi repositories
- Kubernetes-native NiFi coordination and shared state configuration
- `ServiceMonitor` support
- health-gated rolling restart orchestration
- watched Secret and ConfigMap drift detection
- policy-driven cert rotation handling
- hibernation and restore to the prior running replica count
- explicit status conditions and events

## Explicitly Out Of Scope

The MVP does not include:

- NiFi 1.x support
- NiFiKop compatibility
- flow deployment CRDs
- user, group, or policy management CRDs
- NiFi Registry lifecycle management
- backup and restore orchestration
- autoscaling
- multi-cluster federation
- advanced day-2 dataflow operations beyond restart and hibernation safety

## Milestones

### v0.1

- publish the design pack
- scaffold the standalone Helm chart
- add managed-mode chart switches
- define the `NiFiCluster` CRD
- implement target resolution and status-only reconciliation

### v0.2

- implement health-gated `OnDelete` rollout orchestration
- add watched Secret and ConfigMap hash detection
- implement policy-driven cert rotation handling
- add controller metrics and events

### v0.3

- implement hibernation and restore tracking
- implement upgrade coordination
- add kind integration coverage
- add AKS smoke-test guidance and OpenShift-specific notes
