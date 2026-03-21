# Verification and Support Levels

This page explains how NiFi-Fabric support statements are backed by repository verification.

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

Repository verification is centered on:

- the standard managed install through `charts/nifi-platform`
- cert-manager-first TLS
- core lifecycle behavior such as rollout, TLS restart handling, hibernation, and restore
- primary authentication and metrics paths
- controller-owned autoscaling

Environment coverage is centered on:

- AKS as the primary supported target environment for the standard managed install path
- OpenShift as a supported environment for the documented managed install and passthrough `Route` shape

## How to Read Support Claims

Use these pages together:

- [Compatibility](compatibility.md) for supported versions and environments
- [Install with Helm](install/helm.md) for the standard install path
- [Advanced Install Paths](install/advanced.md) for non-standard installs
- [Experimental Features](experimental-features.md) for intentionally non-GA areas

When a feature needs more nuance, the detailed support position belongs on that feature page rather than being repeated everywhere.

## Internal Engineering Checks

The repository also contains narrower maintainer checks and verification workflows used during development and release work.

Those engineering details are useful for maintainers, but they are not the main customer entrypoint. Customer-facing docs should prefer:

- what is supported
- what the standard path is
- where the advanced path begins
Those statements should stay primary over raw verification-command inventories.
