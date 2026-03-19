# Compatibility

NiFi-Fabric targets Apache NiFi `2.0.x` through `2.8.x`.

The supported `2.x` line uses one common feature set. Focused repository runtime anchors stay bounded at `2.0.0` and `2.8.0`, and the broader `2.0.x` through `2.8.x` support claim follows the same feature set plus offline validation across the line.

## Support Categories

| Category | Meaning |
| --- | --- |
| `Supported` | Supported for the documented common NiFi `2.x` feature set. |
| `Production-proven` | Proven beyond focused kind-only coverage. No entries are claimed yet in this repository. |
| `Focused-runtime-proven` | Proven in focused runtime workflows in this repository, usually on kind. |
| `Prepared / render-validated` | Configuration and render path exist, but runtime proof is not yet claimed. |
| `Planned` | Documented direction, not yet implemented as a supported runtime path. |

## Common NiFi 2.x Feature Set

The supported NiFi `2.x` line shares this common feature set:

| Feature group | `2.0.x` through `2.8.x` |
| --- | --- |
| `charts/nifi-platform` managed install | Yes |
| safe rollout | Yes |
| hibernation and restore | Yes |
| cert-manager integration | Yes |
| OIDC | Yes |
| LDAP | Yes |
| native API metrics | Yes |
| exporter metrics | Yes |
| autoscaling advisory | Yes |
| autoscaling enforced scale-up | Yes |
| autoscaling enforced scale-down | Yes |
| KEDA integration | Yes, still experimental |

The table above is intentionally the common set only. Version-specific focused integrations that are currently documented on NiFi `2.8.0`, such as Flow Registry Client workflows, stay documented separately instead of being implied across the whole line.

## Supported NiFi Versions

| NiFi version | Common feature set | Evidence |
| --- | --- | --- |
| `2.0.x` | Supported | Focused runtime anchor on `2.0.0` plus offline validation. |
| `2.1.x` | Supported | Offline validation against the same common feature set. |
| `2.2.x` | Supported | Offline validation against the same common feature set. |
| `2.3.x` | Supported | Offline validation against the same common feature set. |
| `2.4.x` | Supported | Offline validation against the same common feature set. |
| `2.5.x` | Supported | Offline validation against the same common feature set. |
| `2.6.x` | Supported | Offline validation against the same common feature set. |
| `2.7.x` | Supported | Offline validation against the same common feature set. |
| `2.8.x` | Supported | Focused runtime anchor on `2.8.0` plus offline validation. |
| `1.x` | Not supported | Out of scope. |

## Focused Runtime Anchors

Main focused compatibility commands:

- `make kind-nifi-compatibility-fast-e2e`
- `make kind-nifi-compatibility-fast-e2e-reuse`

Shared contract used for both runtime anchors:

- install through `charts/nifi-platform`
- secured cluster startup and health
- basic single-user platform readiness
- native API metrics through the dedicated metrics `Service` and named `ServiceMonitor` resources
- one bounded controller-owned enforced scale-up from `2 -> 3`

The shared harness does not need version-specific values files. It only switches the NiFi image tag inline and keeps controller and chart behavior the same across anchors.

## Install Models

| Install model | Status | Notes |
| --- | --- | --- |
| `charts/nifi-platform` managed install | Focused-runtime-proven | Standard customer path and the primary shared NiFi `2.x` compatibility install surface. |
| `charts/nifi-platform` managed + cert-manager | Focused-runtime-proven | cert-manager must already exist in the cluster. |
| `charts/nifi-platform` standalone mode | Prepared / render-validated | Rendered and supported as a chart shape, but the main product story is managed mode. |
| `charts/nifi` standalone app chart | Focused-runtime-proven | Supported secondary path with dedicated focused proofs outside the shared platform-chart matrix. |

## Feature Compatibility

| Feature | Status | Notes |
| --- | --- | --- |
| Safe rollout | Focused-runtime-proven | Controller-managed, health-gated rollout sequencing. |
| Hibernation and restore | Focused-runtime-proven | Controller-owned lifecycle flow. |
| cert-manager integration | Focused-runtime-proven | cert-manager is a prerequisite. |
| OIDC | Focused-runtime-proven | First-class managed auth option. The richer browser-flow group-claims proof remains conservative on the current local Keycloak `26.x` path. |
| LDAP | Focused-runtime-proven | First-class managed auth option. |
| Native API metrics | Focused-runtime-proven | Primary metrics path and part of the shared NiFi `2.x` compatibility contract. |
| Exporter metrics | Focused-runtime-proven, experimental | Flow metrics only. |
| Site-to-site metrics | Focused-runtime-proven, experimental | Dedicated focused proof exists, but it is intentionally outside the common NiFi `2.x` compatibility line claim on this page. |
| Autoscaling advisory | Focused-runtime-proven | Primary controller-owned recommendation path. |
| Autoscaling enforced scale-up | Focused-runtime-proven | Primary controller-owned execution path and part of the shared NiFi `2.x` compatibility contract as a bounded `2 -> 3` proof. |
| Autoscaling enforced scale-down | Focused-runtime-proven | Production-ready for the bounded one-step, conservative, controller-owned path, with deeper dedicated proof outside the shared version matrix. |
| KEDA integration | Focused-runtime-proven, experimental | Optional external intent source only. |
| GitHub Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| GitLab Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| Bitbucket Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| Azure DevOps Flow Registry Client | Prepared / render-validated | Prepared catalog definition only. |

## Environment Scope

| Environment | Status | Notes |
| --- | --- | --- |
| kind | Focused-runtime-proven | Current runtime proof baseline. |
| AKS | Prepared / render-validated | Primary target environment, but no real-cluster runtime proof is claimed in this slice. |
| OpenShift | Prepared / render-validated | Secondary target environment, with published readiness guidance and no real-cluster runtime proof claimed in this slice. |

## Conservative Claims Left Intentionally Conservative

- no production-proven cloud runtime claim is made yet
- no claim is made beyond Apache NiFi `2.0.x` through `2.8.x`
- no claim is made that version-specific integrations such as Flow Registry Client workflows are supported across the whole line unless this page says so explicitly
- richer ingress-backed OIDC browser-flow proof is still conservative on kind
- KEDA remains experimental even with green focused proof
- smarter drainability selection, richer capacity reasoning, and bulk or multi-node autoscaling policies remain future work
