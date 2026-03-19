# Risk and Troubleshooting Note

The customer-facing operational guidance now lives here:

- [Operations and Troubleshooting](operations.md)
- [Experimental Features](experimental-features.md)
- [Compatibility](compatibility.md)

This file remains only as a compatibility pointer for older links.

Current trust-manager note:

- optional trust-manager integration distributes shared CA bundles only
- cert-manager remains the primary certificate lifecycle path
- optional workload TLS `ca.crt` mirroring is chart-owned helper automation, not controller-owned trust orchestration
- Secret bundle targets depend on upstream trust-manager secret-target support and authorization
- automatic app consumption is still centered on the PEM `ca.crt` bundle key even when PKCS12 or JKS outputs are rendered

Current operations-package note:

- the new dashboard, alert rules, and runbooks are starter assets, not a full production observability pack
- the starter alert file intentionally avoids hard-coding environment-specific scrape job names and custom-resource export assumptions
- native API scrape-failure alerts and KEDA external-intent alerts usually need environment-specific adaptation
- teams should still add their own alerts for ingress, storage, node pressure, and cloud control-plane dependencies

Current autoscaling note:

- enforced scale-down remains intentionally conservative and one-step-at-a-time
- the stronger low-pressure model lowers false-positive removals, but it can also delay legitimate scale-down when NiFi backlog or thread evidence is incomplete
- stuck disconnect, offload, or post-removal settle work now surfaces more explicitly, but the controller still prefers safe blocking and operator intervention over aggressive remediation
- blocked scale-down execution is restart-safe and resumable; failed execution still requires operator attention before trusting another destructive step
- operators should still size cooldowns, stabilization windows, and minimum replicas for their workload rather than expecting aggressive node removal
- bulk or multi-node removal remains deferred because removing another node before the previous step has fully settled would weaken the current safety model
- any future bulk policy would still need to stop immediately on degraded state, blocked execution, missing candidate resolution, or higher-precedence lifecycle work rather than pushing through a larger target

Current DR note:

- declarative recovery and stateful data recovery are intentionally separate concerns
- the product can restore release intent, but not queued data or repository state without operator-owned storage recovery
- PVC snapshot cadence, retention, restore testing, and storage-class behavior remain operator-owned
- cert-manager, trust-manager, Secret-manager, and IdP recovery plans remain separate dependencies in a production DR design
- hibernation and restore are lifecycle features, not a substitute for backup or cross-cluster disaster recovery
