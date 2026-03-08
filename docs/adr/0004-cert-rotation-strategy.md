# ADR 0004: Cert Rotation Strategy

- Status: Accepted
- Date: 2026-03-08

## Context

NiFi clusters must remain secure by default, and TLS rotation is one of the highest-risk operational paths. cert-manager can rotate certificate contents without changing Secret names. NiFi 2 also supports TLS autoreload capability when the mounted material, paths, and passwords remain compatible.

The platform needs a predictable policy for when to observe autoreload and when to force a controlled restart.

## Decision

The default strategy is policy-driven and autoreload-first.

The chart should assume:

- cert-manager-managed TLS material
- stable Secret references
- stable mounted file paths
- `nifi.security.autoreload.enabled=true`

The controller evaluates TLS drift and follows this policy:

- if Secret contents change but references, paths, and passwords remain the same, prefer an autoreload-first observation window
- if TLS references change, mount paths change, or sensitive password material changes, require a controlled rolling restart
- if cluster or pod health degrades after TLS drift, require a controlled rolling restart
- if explicit restart policy demands restart on TLS drift, perform the controlled restart even if autoreload may succeed

Current implementation details:

- stable content drift uses a `30s` autoreload observation window
- `ObserveOnly` records drift and never forces restart for content-only drift
- `AutoreloadThenRestartOnFailure` escalates to restart only when health degrades or the stable-health gate still fails after observation
- `AlwaysRestart` skips observation for content-only drift

## Consequences

- Routine certificate renewal can stay low-disruption.
- Material TLS topology changes still receive explicit, safe orchestration.
- The controller keeps the decision logic centralized and visible instead of relying on undocumented assumptions.
