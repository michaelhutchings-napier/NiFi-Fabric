# Verification and Support Levels

This page explains how NiFi-Fabric support claims are backed by repository verification.

## Verification Layers

NiFi-Fabric uses three main layers:

- unit and envtest coverage for controller behavior
- Helm lint and render coverage for chart behavior
- focused runtime verification for the standard product paths

## What This Means for Customers

The project does not treat “renders successfully” and “runtime-proven” as the same thing.

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

## How to Read Support Claims

Use these pages together:

- [Compatibility](compatibility.md) for supported versions and environments
- [Install with Helm](install/helm.md) for the standard install path
- [Advanced Install Paths](install/advanced.md) for non-standard installs
- [Experimental Features](experimental-features.md) for intentionally non-GA areas

When a feature needs more nuance, the detailed support position belongs on that feature page rather than being repeated everywhere.

## Internal Verification Detail

This repository also contains focused runtime workflows, proof commands, and narrower validation paths used by maintainers.

Those details are useful for engineering work, but they are not the main customer entrypoint. Customer-facing docs should prefer:

- what is supported
- what the standard path is
- where the advanced path begins

over raw proof-command inventories.
