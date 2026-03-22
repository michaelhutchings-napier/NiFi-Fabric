# ADR 0007: Flow Upgrade Advisor Boundaries

- Status: Accepted
- Date: 2026-03-22

## Context

Teams migrating Apache NiFi flows need a repeatable way to inspect older flow definitions, understand version-to-version breaking changes, apply safe rewrites, and publish upgraded flows into a supported NiFi `2.x` delivery path.

That need is real, but putting flow migration logic into the runtime controller or a new CRD would widen the product into a broad flow-management operator. That would conflict with the project scope, Helm-first boundaries, and the goal of keeping the platform explainable.

## Decision

NiFi-Fabric will treat flow upgrade assistance as external-first offline tooling.

The product direction is:

- build a dedicated Flow Upgrade Advisor as a separate reusable CLI or service, expected to live in its own repository
- allow this repository to carry a thin wrapper under `tools/flow-upgrade-advisor/` for pinned versions, examples, and NiFi-Fabric-specific integration
- keep upgrade analysis, rewrite, validation, and publish steps explicit and operator-invoked
- keep runtime deployment on the existing NiFi-Fabric paths such as Git-based registry clients and `versionedFlowImports.*`

The platform will not:

- add a flow-upgrade CRD
- run migration logic in the controller reconciliation loop
- mutate live flows continuously as part of cluster management
- change its runtime support position of Apache NiFi `2.x` only

Reading NiFi `1.x` flow artifacts offline for migration analysis is allowed. Running NiFi `1.x` as a platform target is not.

The tool should produce:

- a machine-readable migration report
- a human-readable summary of auto-fixes, manual changes, and blocked items
- an optionally rewritten target flow artifact
- an explicit publish step to a registry or Git-backed flow source

Rule coverage should be version-pair-specific and explicit rather than pretending all source and target pairs are equivalent.

## Consequences

- The platform stays small and avoids a second control plane.
- GitOps users get reviewable artifacts and reports before deployment.
- Version coverage can grow incrementally by adding tested rule packs.
- The project accepts ongoing maintenance for migration rules, fixtures, and target-version validation.
- Testing for this tooling should be fixture-driven, with coverage notes for no-op, auto-fix, manual-change, and blocked outcomes per supported version pair.
