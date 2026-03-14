# Roadmap

NiFi-Fabric keeps the roadmap small and explicit.

## Production-Ready Now

- one-release platform install with `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model for rollout, TLS handling, hibernation, and restore
- supported external Secret and cert-manager TLS paths
- first-class OIDC and LDAP auth options
- native API metrics as the primary metrics mode
- Git-based Flow Registry Client direction through chart-managed catalog rendering

## Experimental

- controller-owned enforced autoscaling scale-down
- KEDA as an optional external autoscaling intent source

## Prepared-Only

- site-to-site metrics mode
- broader environment-specific install wrappers beyond Helm

## Planned Next

- broader NiFi `2.x` compatibility proof
- broader cloud environment proof, especially AKS
- more environment-specific operational guides
- broader metrics-family proof where it stays smaller than a full metrics platform
