# Flow Upgrade Advisor Wrapper

This directory reserves the NiFi-Fabric entrypoint for the planned external Flow Upgrade Advisor.

The full design lives in `docs/internals/flow-upgrade-advisor.md`.

## Intended Role

The core migration engine should live in a separate repository.

This repository should keep only thin integration assets here, such as:

- pinned tool or container image versions
- sample configuration files
- helper scripts for local or CI use
- examples that hand upgraded artifacts off to NiFi-Fabric deployment workflows

## Boundary

This wrapper is offline tooling only.

It must not:

- become controller logic
- run inside the reconciliation loop
- introduce a new CRD
- turn NiFi-Fabric into a generic flow migration platform

## Expected Workflow

1. Analyze a source flow artifact against a selected target NiFi version.
2. Review the generated migration report.
3. Apply safe rewrites when available.
4. Validate the upgraded result.
5. Publish to a Git-backed registry or NiFi Registry.
6. Deploy through existing NiFi-Fabric paths such as `versionedFlowImports.*`.
