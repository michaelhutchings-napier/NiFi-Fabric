# Risks and Limits

NiFi-Fabric keeps the standard install path small and supportable, but production teams should still plan around a few operational realities.

## What To Plan For

- stateful recovery is still an operator responsibility; NiFi-Fabric can restore the intended cluster shape, but it does not recover lost queue data or repository contents without storage-level recovery
- cert-manager, issuers, identity providers, ingress, storage classes, and other platform dependencies remain separate systems that must be operated and backed up in their own right
- lifecycle automation is intentionally conservative; rollout, TLS restart handling, hibernation, restore, and scale-down prefer safe blocking over aggressive automation
- the shipped dashboards, alerts, and runbooks are starter assets, not a complete production observability package
- optional integrations such as KEDA and trust-manager stay bounded and should be enabled only when they fit your environment

## Where To Read Next

- [Operations and Troubleshooting](operations.md)
- [Disaster Recovery](dr.md)
- [Compatibility](compatibility.md)
- [Experimental Features](experimental-features.md)

## Older Links

This page remains as a short landing page for older links.

Detailed risk, support-boundary, and troubleshooting guidance now lives in the pages above.
