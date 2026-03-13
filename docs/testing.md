# Verification and Support Levels

This page explains how NiFi-Fabric support claims are grounded in repository proof.

## Verification Layers

NiFi-Fabric uses several layers of verification:

- unit and envtest coverage for controller behavior
- Helm render and lint coverage for chart behavior
- focused kind runtime workflows for supported feature paths

## What Is Proven Today

Focused runtime proof in this repository includes:

- standard managed platform install on kind
- cert-manager integration on kind
- OIDC and LDAP focused auth paths on kind
- native API metrics on kind
- exporter metrics on kind
- controller-owned autoscaling focused flows on NiFi `2.8.0`
- optional experimental KEDA intent-source flows on NiFi `2.8.0`
- GitHub and GitLab Flow Registry Client focused flows on NiFi `2.8.0`

## What Is Render-Validated or Prepared

- site-to-site metrics remains prepared-only
- AKS guidance is published but still conservative
- OpenShift guidance is published but still conservative
- Bitbucket and Azure DevOps Flow Registry Client definitions are prepared and render-validated

## Customer Meaning of Support Levels

Use the categories in [Compatibility](compatibility.md):

- `Focused-runtime-proven` means the feature is exercised in focused runtime workflows in this repository
- `Prepared / render-validated` means the shape is intentionally documented and rendered, but the repo does not claim runtime proof yet
- `Production-proven` is reserved for broader runtime proof than the current focused kind baseline

## Validation Used for Documentation Consistency

Customer-facing docs should stay aligned with:

- `go test ./...`
- `helm lint charts/nifi`
- `helm lint charts/nifi-platform`
- `helm template` for the standard chart install paths
- focused checks for the feature being documented

## Current Conservative Boundaries

- the repo does not yet claim a production-proven cloud runtime matrix
- AKS and OpenShift remain conservative until real-cluster proof is recorded
- experimental features stay explicitly marked experimental even when focused runtime proof exists
