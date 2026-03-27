# Verification and Support Levels

This page explains how NiFi-Fabric support statements are backed by project verification.

## Verification Layers

NiFi-Fabric uses three main layers:

- unit and envtest coverage for controller behavior
- Helm lint and render coverage for chart behavior
- runtime verification for the standard product paths

## What This Means for Customers

The project does not treat “renders successfully” and “verified in a running environment” as the same thing.

In practice:

- the standard install path has the strongest verification focus
- advanced paths are supported, but some are documented more conservatively
- feature-specific support detail lives on the relevant install, manage, and compatibility pages

## Standard Verified Areas

Project verification is centered on:

- the standard managed install through `charts/nifi-platform`
- cert-manager-first TLS
- core lifecycle behavior such as rollout, TLS restart handling, hibernation, and restore
- primary authentication and metrics paths
- controller-owned autoscaling

Environment coverage is centered on:

- AKS as the primary supported target environment
- OpenShift as a supported environment, including OpenShift `Route` for external HTTPS access

## How to Read Support Claims

Use these pages together:

- [Compatibility](compatibility.md) for supported versions and environments
- [Install with Helm](install/helm.md) for the standard install path
- [Advanced Install Paths](install/advanced.md) for non-standard installs
- [Experimental Features](experimental-features.md) if any customer-facing experimental areas are introduced later

When a feature needs more nuance, the detailed guidance belongs on that feature page rather than being repeated everywhere.

## Engineering Checks

The project also contains narrower engineering checks and verification workflows used during development and release work.

Examples include:

- the dedicated flow-action audit kind proof lane in `.github/workflows/flow-action-audit-kind-e2e.yaml`
- focused artifact publication lanes for secondary runtime components when those components are part of a supported product path

Those engineering details are useful for maintainers, but they are not the main customer entrypoint. Customer-facing docs should prefer:

- what is supported
- what the standard path is
- where the advanced path begins
Those statements should stay primary over raw verification-command inventories.
