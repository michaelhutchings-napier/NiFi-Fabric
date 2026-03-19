# Roadmap

NiFi-Fabric keeps the roadmap small and explicit.

## Production-Ready Now

- one-release platform install with `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model for rollout, TLS handling, hibernation, and restore
- built-in controller-owned autoscaling, including advisory recommendations, enforced scale-up, richer bounded capacity reasoning, actual StatefulSet removal-candidate qualification, and bounded sequential multi-step scale-down
- supported external Secret and cert-manager TLS paths
- first-class OIDC and LDAP auth options
- native API metrics as the primary metrics mode
- Git-based Flow Registry Client direction through chart-managed catalog rendering

## Experimental

- KEDA as an optional external autoscaling intent source

## Prepared-Only

- site-to-site metrics mode
- broader environment-specific install wrappers beyond Helm

## Planned Next

- broader NiFi `2.x` compatibility proof
- broader cloud environment proof, especially AKS
- more environment-specific operational guides
- broader metrics-family proof where it stays smaller than a full metrics platform
- broader per-node drainability ranking only if it stays explainable, bounded, and justified by trustworthy evidence beyond the current actual-removal-candidate qualification model
- broader bulk autoscaling policy depth beyond the current bounded sequential-episode model only if it remains sequential controller-owned one-node steps with fresh settle and requalification after every removal
- broader KEDA maturity if the optional external-intent path proves valuable
