# Flow Upgrade Advisor Design

## Summary

NiFi-Fabric should support a Flow Upgrade Advisor as external-first tooling, not as a controller feature.

The tool solves a narrow problem:

- inspect a source flow artifact
- compare it to a selected target NiFi version
- report what changed
- apply deterministic safe rewrites when possible
- validate the rewritten result
- publish the upgraded flow explicitly

This keeps the runtime product simple while still giving users a practical migration path.

## Why This Exists

Apache NiFi provides migration guidance, deprecation logging, flow analysis rules, versioned flows, and registry integration, but not one upstream open-source tool that covers the full workflow end to end.

Users want one repeatable flow:

1. select source and target versions
2. load a user flow
3. see the impact
4. auto-fix what is safe
5. validate the result
6. publish it into the right registry or Git-backed source

## Product Shape

Use two layers:

- dedicated repo: the main reusable tool, likely named `nifi-flow-upgrade-advisor`
- repo-local wrapper: `tools/flow-upgrade-advisor/` in this repository for pinned versions, example configs, and NiFi-Fabric workflow integration

The current repository should not own the core migration engine long term. It should only make the tool easy to use alongside NiFi-Fabric.

## Core Principles

- offline first
- explicit source and target version selection
- version-pair rule packs instead of hidden generic behavior
- safe deterministic rewrites only
- human-readable reports before publish
- publish is explicit, never a side effect of analysis
- no new CRDs
- no controller-managed flow migration
- no broad live graph management

## Supported Workflow

### 1. Load

Supported source inputs should include:

- `flow.json.gz`
- exported versioned flow snapshots
- Git-backed registry flow directories
- NiFi Registry exported flow versions

Optional side inputs:

- source NiFi version
- target NiFi version
- source extension inventory
- target extension inventory
- parameter-context exports

### 2. Analyze

The tool builds a migration plan by combining:

- version-pair rules
- component and bundle availability checks
- property and allowable-value changes
- parameter and variable compatibility checks
- extension coordinate renames or removals

The report should classify findings as:

- `auto-fix`
- `manual-change`
- `manual-inspection`
- `blocked`
- `info`

### 3. Rewrite

The tool may apply safe rewrites such as:

- component type renames with compatible configuration mapping
- property renames
- removed property cleanup where the replacement is unambiguous
- bundle coordinate updates
- variable-to-parameter conversion scaffolding when deterministic

The tool must not guess through domain-specific intent. If a rewrite depends on business meaning, credentials, or ambiguous routing logic, it should stay manual.

### 4. Validate

Validation should happen before publish and should be runnable in CI.

Validation layers:

- artifact schema validation
- extension availability against the selected target
- reference validation for controller services and parameter contexts
- optional dry-run import into an ephemeral target NiFi `2.x` instance

### 5. Publish

Publishing should be a separate command and should support:

- filesystem output only
- Git-backed registry layout for GitHub, GitLab, or Bitbucket workflows
- NiFi Registry import

### 6. Deploy

NiFi-Fabric deployment stays on the existing product paths:

- Flow Registry Client catalogs
- Git-based registry workflows
- `versionedFlowImports.*`

The upgrade tool prepares artifacts. NiFi-Fabric deploys them.

## CLI Shape

The core CLI should stay boring and scriptable:

```text
nifi-flow-upgrade analyze  --source ... --source-version ... --target-version ...
nifi-flow-upgrade rewrite  --plan migration-report.json --out ./out
nifi-flow-upgrade validate --input ./out --target-version ...
nifi-flow-upgrade publish  --input ./out --publisher git|nifi-registry|fs ...
```

Optional convenience command:

```text
nifi-flow-upgrade run --source ... --source-version ... --target-version ... --publish ...
```

`run` should still emit the intermediate report and rewritten artifacts, not hide them.

## Rule Engine

The migration engine should use explicit rule packs keyed by source and target versions.

Recommended rule categories:

- `component-removed`
- `component-replaced`
- `bundle-renamed`
- `property-renamed`
- `property-value-changed`
- `property-removed`
- `variable-migration`
- `manual-inspection`
- `blocked`

Recommended rule fields:

- source version range
- target version range
- component or bundle selector
- matching property selector
- message
- optional deterministic transform
- evidence or reference URL

This gives predictable growth from `1.27 -> 2.0`, then `2.0 -> 2.4`, `2.4 -> 2.8`, and so on, instead of pretending one huge ruleset covers every path equally well.

## Outputs

Minimum outputs:

- `migration-report.json`
- `migration-report.md`
- rewritten flow artifact in target format
- optional generated parameter-context patch data
- optional publish metadata for Git or registry import

The JSON report should be stable enough for CI filtering. The Markdown report should be optimized for pull requests and human review.

## NiFi-Fabric Integration

This repository should only add thin integration:

- pinned tool version or container image reference
- sample configs for supported NiFi-Fabric workflows
- examples that publish to Git-backed registries or NiFi Registry
- optional generated values snippet for `versionedFlowImports.*`

Useful generated output for this repo:

- a versioned flow artifact ready for Git-based registry storage
- a values overlay snippet pointing at the upgraded flow version
- a report that can be attached to a GitOps pull request

## Non-Goals

- live synchronization of user flows
- in-cluster mutation of arbitrary process groups
- user and policy migration
- generic registry administration
- support for running NiFi `1.x` in NiFi-Fabric
- a second deployment API surface

## MVP

### Phase 1

- analyze source flow artifacts
- emit structured report
- no publish automation beyond filesystem output

### Phase 2

- add deterministic rewrites
- add target extension validation
- emit PR-friendly Markdown summaries

### Phase 3

- add Git-backed registry publishers
- add NiFi Registry publisher
- generate NiFi-Fabric values snippets for deployment handoff

## Verification Notes

Each supported version pair needs fixture coverage for:

- no-op compatible flow
- auto-fix case
- manual-change case
- blocked case

End-to-end validation should cover:

- upgrade a known source fixture
- validate against a target NiFi `2.x` image
- publish the result
- deploy it through the documented NiFi-Fabric import path on kind
