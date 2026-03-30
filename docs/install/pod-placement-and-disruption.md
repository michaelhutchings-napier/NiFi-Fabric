# Pod Placement And Disruption

NiFi-Fabric already exposes the standard Kubernetes placement and disruption
controls most teams need. This page gives copy-paste-safe starting points for
using them with the NiFi workload.

## Available Controls

The app chart already exposes:

- `affinity`
- `nodeSelector`
- `tolerations`
- `topologySpreadConstraints`
- `priorityClassName`
- `pdb.*`

Use these as normal Kubernetes primitives. NiFi-Fabric does not add a second
placement abstraction or controller-specific scheduling logic on top of them.

## Pod Disruption Budget Default

The default chart PDB is:

```yaml
pdb:
  enabled: true
  minAvailable: 1
```

That means voluntary disruptions such as node drains must leave at least one
NiFi pod available.

This is a reasonable default because it:

- keeps the protection small and explainable
- avoids full voluntary eviction of the NiFi workload
- works for both standalone and managed installs

You may want to change it when:

- development or kind environments need faster reruns and you are comfortable
  with lower protection
- larger production clusters want a higher `minAvailable`
- your maintenance model needs different voluntary-disruption behavior than the
  default

## Placement Patterns

### Zone Spread Baseline

Use topology spread when you want pods distributed across zones or nodes
without forcing strict scheduling failure when the topology is temporarily
imbalanced.

See:

- [platform-managed-zone-spread-values.yaml](../../examples/platform-managed-zone-spread-values.yaml)

This pattern:

- spreads NiFi pods by zone
- also spreads by hostname as a local fallback
- keeps `whenUnsatisfiable: ScheduleAnyway` so short-lived imbalance does not
  block scheduling

### Stricter Anti-Affinity Baseline

Use required pod anti-affinity when you want to prevent multiple NiFi pods from
landing on the same Kubernetes node.

See:

- [platform-managed-strict-anti-affinity-values.yaml](../../examples/platform-managed-strict-anti-affinity-values.yaml)

This pattern:

- requires one NiFi pod per node
- is stricter than topology spread
- can leave pods Pending if the cluster does not have enough suitable nodes

Use it only when the cluster capacity and node labels are well understood.

## Choosing Between Them

Prefer topology spread when:

- you want a safe baseline
- temporary imbalance is acceptable
- the cluster autoscaler or node availability may vary

Prefer strict anti-affinity when:

- one pod per node is a real requirement
- the cluster has enough capacity to satisfy it
- Pending pods are an acceptable signal when that requirement cannot be met

## Node Selection And Taints

Use `nodeSelector` and `tolerations` the same way you would for any other
stateful workload.

Common reasons:

- pinning NiFi onto storage-capable nodes
- isolating NiFi onto a dedicated node pool
- matching tainted worker pools reserved for data workloads

Keep these choices explicit in the same values overlay as the related
affinity or spread settings.

## Operator Notes

- start with topology spread before moving to strict anti-affinity
- treat stricter placement as a capacity commitment, not just a preference
- review the PDB together with your drain and maintenance procedures
- for small local clusters, disabling the PDB can be reasonable for faster test
  cycles, but do not carry that into production by accident

## Non-Goals

- not scheduler customization
- not heterogeneous role-group management
- not controller-managed placement logic
