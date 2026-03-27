# Roadmap

NiFi-Fabric keeps the roadmap small and explicit.

## Production-Ready Now

- one-release platform install with `charts/nifi-platform`
- cert-manager-first managed quickstart install through `charts/nifi-platform`
- standalone app install with `charts/nifi`
- thin controller model for rollout, TLS handling, hibernation, and restore
- standard pod placement and disruption controls through `affinity`, `topologySpreadConstraints`, and pod disruption budget settings
- built-in controller-owned autoscaling, including advisory recommendations, enforced scale-up, richer capacity reasoning, actual StatefulSet removal-candidate qualification, and sequential multi-step safe scale-down
- optional KEDA external intent through `NiFiCluster` `/scale`, with controller-owned execution and starter operational support
- supported external Secret and cert-manager TLS paths
- first-class OIDC and LDAP auth options
- native API metrics as the primary metrics mode
- exporter metrics as an optional GA secondary metrics mode
- typed Site-to-Site metrics export as an optional GA sender-side path
- typed Site-to-Site status export as an optional GA sender-side path
- typed Site-to-Site provenance export as an optional GA sender-side path
- bounded flow-change audit with NiFi-native local history plus optional `export.type=log`
- Git-based Flow Registry Client direction through chart-managed catalog rendering
- Parameter Context management
- versioned-flow import
- NiFi Registry integration through a typed `provider=nifiRegistry` client plus platform-chart versioned-flow import
- generic extra volumes, mounts, and sidecars for environment-specific extensions

## Planned Next

- more Layer 7 support for the Istio Ambient service mesh profile
- broader per-node drainability ranking only if it stays explainable, conservative, and justified by trustworthy evidence beyond the current actual-removal-candidate qualification model
- broader bulk autoscaling policy depth beyond the current sequential scale-down episode model only if it remains sequential controller-owned one-node steps with fresh settle and requalification after every removal
