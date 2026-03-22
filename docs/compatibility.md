# Compatibility

NiFi-Fabric targets Apache NiFi `2.0.x` through `2.8.x`.

The project keeps one common product model across that supported NiFi 2.x line. It does not maintain different controller or chart behavior for different NiFi versions.

## Supported NiFi Versions

| NiFi version | Status |
| --- | --- |
| `2.0.x` through `2.8.x` | Supported |
| `1.x` | Not supported |

## Supported Kubernetes Versions

NiFi-Fabric supports Kubernetes `1.21+`.

## Supported Helm Versions

NiFi-Fabric requires Helm `3.x` or `4.x` for chart-based installs.

The project charts use Helm chart `apiVersion: v2`, so Helm `2.x` is not supported.

## Standard Install Path

The standard customer path is:

- `charts/nifi-platform`
- managed mode
- cert-manager-first TLS

That is the primary install story for support and documentation.

## Common Supported Feature Set

Across the supported NiFi `2.x` line, NiFi-Fabric supports:

- standard managed install through `charts/nifi-platform`
- safe rollout
- hibernation and restore
- cert-manager integration
- OIDC and LDAP
- native API metrics
- exporter metrics
- controller-owned autoscaling
- optional KEDA integration

Some more specialized integrations are documented separately and should not be assumed across every version unless that page says so.

Environment guidance:

- AKS is the primary supported target environment.
- OpenShift is supported.
- NiFi-Fabric should work on any conformant Kubernetes-based cloud platform with the required storage, networking, and image access.

## Environment Position

| Environment | Current position |
| --- | --- |
| kind | primary local validation environment |
| AKS | primary supported target environment |
| OpenShift | supported |

See:

- [AKS](aks.md)
- [OpenShift](openshift.md)
- [Authentication](manage/authentication.md)

## Support Reading

Use these pages together:

- [Features](features.md) for the product surface
- [Verification and Support Levels](testing.md) for how support claims are grounded
- [Experimental Features](experimental-features.md) if any customer-facing experimental areas are introduced later

For detailed feature-specific support details, use the relevant install and manage pages rather than relying on this page alone.
