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
