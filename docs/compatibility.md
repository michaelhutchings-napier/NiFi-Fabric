# Compatibility

NiFi-Fabric targets Apache NiFi `2.0.0` through `2.8.x`.

Other NiFi `2.x` versions are expected to work unless noted, but the repository only claims focused runtime proof where it is explicitly listed.

## Support Categories

| Category | Meaning |
| --- | --- |
| `Production-proven` | Proven beyond focused kind-only coverage. No entries are claimed yet in this repository. |
| `Focused-runtime-proven` | Proven in focused runtime workflows in this repository, usually on kind. |
| `Prepared / render-validated` | Configuration and render path exist, but runtime proof is not yet claimed. |
| `Planned` | Documented direction, not yet implemented as a supported runtime path. |

## Apache NiFi Versions

| NiFi version | Status | Notes |
| --- | --- | --- |
| `2.0.0` | Focused-runtime-proven | Baseline product image tag and standard kind proof target. |
| `2.8.0` | Focused-runtime-proven | Focused compatibility, autoscaling, auth, cert-manager, and Flow Registry Client proof target. |
| Other `2.x` | Prepared / expected | Expected to work unless noted, but not yet runtime-proven here. |
| `1.x` | Not supported | Out of scope. |

## Install Models

| Install model | Status | Notes |
| --- | --- | --- |
| `charts/nifi-platform` managed install | Focused-runtime-proven | Standard customer path. |
| `charts/nifi-platform` managed + cert-manager | Focused-runtime-proven | cert-manager must already exist in the cluster. |
| `charts/nifi-platform` standalone mode | Prepared / render-validated | Rendered and supported as a chart shape, but the main product story is managed mode. |
| `charts/nifi` standalone app chart | Focused-runtime-proven | Supported secondary path. |

## Feature Compatibility

| Feature | Status | Notes |
| --- | --- | --- |
| Safe rollout | Focused-runtime-proven | Controller-managed, health-gated rollout sequencing. |
| Hibernation and restore | Focused-runtime-proven | Controller-owned lifecycle flow. |
| cert-manager integration | Focused-runtime-proven | cert-manager is a prerequisite. |
| OIDC | Focused-runtime-proven | First-class managed auth option. |
| LDAP | Focused-runtime-proven | First-class managed auth option. |
| Native API metrics | Focused-runtime-proven | Primary metrics path. |
| Exporter metrics | Focused-runtime-proven, experimental | Flow metrics only. |
| Site-to-site metrics | Prepared / render-validated | Prepared-only contract, not runtime-enabled. |
| Autoscaling advisory | Focused-runtime-proven | Primary controller-owned recommendation path. |
| Autoscaling enforced scale-up | Focused-runtime-proven | Primary controller-owned execution path. |
| Autoscaling enforced scale-down | Focused-runtime-proven, experimental | One-step, conservative, controller-owned path. |
| KEDA integration | Focused-runtime-proven, experimental | Optional external intent source only. |
| GitHub Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| GitLab Flow Registry Client | Focused-runtime-proven | NiFi `2.8.0`. |
| Bitbucket Flow Registry Client | Prepared / render-validated | Prepared catalog definition only. |
| Azure DevOps Flow Registry Client | Prepared / render-validated | Prepared catalog definition only. |

## Environment Scope

| Environment | Status | Notes |
| --- | --- | --- |
| kind | Focused-runtime-proven | Current runtime proof baseline. |
| AKS | Prepared / render-validated | Primary target environment, but real-cluster proof is still conservative. |
| OpenShift | Prepared / render-validated | Secondary target environment, with published readiness guidance. |

## Conservative Claims Left Intentionally Conservative

- no production-proven cloud runtime claim is made yet
- no blanket claim is made for all NiFi `2.x` versions beyond the explicitly proven versions
- KEDA remains experimental even with green focused proof
- autoscaling scale-down remains experimental
- site-to-site metrics remain prepared-only
