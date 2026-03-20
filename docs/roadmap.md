# Roadmap

NiFi-Fabric keeps the roadmap small and explicit.

## Production-Ready Now

- one-release platform install with `charts/nifi-platform`
- cert-manager-first managed quickstart install through `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model for rollout, TLS handling, hibernation, and restore
- built-in controller-owned autoscaling, including advisory recommendations, enforced scale-up, richer bounded capacity reasoning, actual StatefulSet removal-candidate qualification, and bounded sequential multi-step scale-down
- optional KEDA external intent through `NiFiCluster` `/scale`, with controller-owned execution and starter operational support
- supported external Secret and cert-manager TLS paths
- first-class OIDC and LDAP auth options
- native API metrics as the primary metrics mode
- exporter metrics as an optional GA secondary metrics mode
- typed Site-to-Site metrics export as an optional GA bounded sender-side path
- typed Site-to-Site status export as an optional GA bounded sender-side path
- typed Site-to-Site provenance export as an optional GA bounded sender-side path
- Git-based Flow Registry Client direction through chart-managed catalog rendering
- bounded Parameter Context management
- bounded versioned-flow import
- bounded NiFi Registry compatibility path through a typed `provider=nifiRegistry` client plus platform-chart versioned-flow import on NiFi `2.8.0`

## Experimental

- Route-backed external-host OIDC until a real Route/router runtime proof path is recorded

## Prepared-Only

- broader environment-specific install wrappers beyond Helm

## Planned Next

- broader NiFi `2.x` compatibility proof
- broader cloud environment proof, especially AKS and OpenShift
- finish the standard cert-manager-first install path as a true polished one-command customer flow, including clearer post-install access and day-1 checks
- more environment-specific operational guides
- a small GKE setup guide for the standard install path
- more Layer 7 support for the bounded Istio Ambient service mesh profile
- broader exporter metrics-family proof where it stays smaller than a full metrics platform
- broader per-node drainability ranking only if it stays explainable, bounded, and justified by trustworthy evidence beyond the current actual-removal-candidate qualification model
- broader bulk autoscaling policy depth beyond the current bounded sequential-episode model only if it remains sequential controller-owned one-node steps with fresh settle and requalification after every removal
