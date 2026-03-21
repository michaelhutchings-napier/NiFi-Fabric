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

Current OpenShift auth/runtime position:

- the native external shape is the passthrough `Route`
- OIDC is runtime-proven on OpenShift through that Route shape with named bundle mapping
- LDAP is runtime-proven on OpenShift through that Route shape on the documented bootstrap-admin identity path
- LDAP group-bootstrap and LDAP named bundle mapping are not runtime-proven on OpenShift in this slice

## Environment Position

| Environment | Current position |
| --- | --- |
| kind | primary repository verification baseline |
| AKS | primary target environment |
| OpenShift | supported second target, with focused managed runtime proofs for the native passthrough Route baseline plus OIDC and LDAP auth slices |

See:

- [AKS Readiness Guide](aks.md)
- [OpenShift Baseline Guide](openshift.md)
- [Authentication](manage/authentication.md)

## Support Reading

Use these pages together:

- [Features](features.md) for the product surface
- [Verification and Support Levels](testing.md) for how support claims are grounded
- [Experimental Features](experimental-features.md) for what is intentionally outside the main support path

For detailed feature-specific support boundaries, use the relevant install and manage pages rather than relying on this page alone.
