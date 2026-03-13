# KEDA Integration Position

## Decision

KEDA is now implemented as an optional experimental input source for autoscaling intent.

It is not the primary autoscaler and it is not the executor. The supported shape is deliberately narrow:

- KEDA targets the `NiFiCluster` custom resource through its Kubernetes `/scale` surface
- KEDA writes external replica intent into the `NiFiCluster` control plane
- the `NiFiCluster` controller remains the only component that mutates the NiFi `StatefulSet`
- external scale-down intent is observed but not executed; safe scale-down stays controller-native

## Non-Negotiable Constraints

- the `NiFiCluster` controller remains the only executor of replica changes
- KEDA or HPA must not directly own `StatefulSet.spec.replicas`
- scale-down must continue to reuse disconnect, offload, and highest-ordinal-first deletion sequencing
- hibernation, restore, rollout, TLS restart, degraded state, and autoscaling execution precedence stay controller-owned
- any future KEDA integration must remain optional and must not create a hidden second autoscaler

## Option Comparison

### 1. KEDA as Design-Only or Future Integration

Shape:

- document how KEDA could fit later
- do not add CRD fields, examples that look supported, or runtime wiring yet

Pros:

- preserves the current single control plane without compromise
- avoids prematurely adding an awkward API just to satisfy KEDA's scaling model
- keeps the project honest about what is supported today

Cons:

- users do not get a KEDA runtime path yet

Recommendation:

- acceptable only if the controller-owned scale surface did not exist yet
- no longer the chosen slice because a bounded controller-mediated path now exists without adding a second executor

### 2. KEDA as an External Recommendation or Intent Source

Shape:

- KEDA produces a replica recommendation from an external trigger
- the controller consumes that recommendation and decides whether to act
- the controller still enforces health gates, precedence, cooldown, and safe scale-down sequencing

Pros:

- preserves controller-owned execution
- lets KEDA stay optional and focused on trigger ecosystems instead of destructive orchestration
- fits the existing advisory-then-enforced controller model better than direct scaling

Cons:

- KEDA is built around driving a target through the Kubernetes scaling path, so the controller must expose a clear scale surface and GitOps teams must understand that this field is runtime-owned
- the generated HPA still exists, so the integration must explicitly prevent direct `StatefulSet` ownership and document scale-down rejection
- GitOps tools must ignore or accept drift on the controller-owned external intent field when KEDA is enabled

Recommendation:

- chosen implementation model
- first acceptable scope is scale-up intent only; scale-down remains controller-native

### 3. KEDA Targeting an Intermediate Controller-Owned Object or Path

Shape:

- KEDA targets a controller-owned scale surface instead of the `StatefulSet`
- the controller interprets that surface as capacity intent, not as permission to mutate pods directly

Examples:

- the implemented `/scale` subresource on `NiFiCluster`
- the controller-owned `spec.autoscaling.external.requestedReplicas` field behind that scale surface
- a future dedicated controller-owned intent object if the current shape ever proves too awkward

Pros:

- can preserve controller-owned execution if designed well
- gives KEDA a native Kubernetes target instead of requiring side-channel glue

Cons:

- still introduces another policy writer through KEDA and the generated HPA
- adds a controller-owned API surface that GitOps tools must treat as runtime-managed when enabled
- requires explicit status so users can distinguish external intent from controller execution

Recommendation:

- acceptable only in a narrow form
- implemented here as a bounded external scale-up intent contract on `NiFiCluster`

### 4. Direct KEDA or HPA Scaling of the StatefulSet

Shape:

- KEDA `ScaledObject` or HPA targets the NiFi `StatefulSet` directly

Pros:

- simplest to wire technically
- uses KEDA as documented out of the box

Cons:

- KEDA and HPA would mutate the workload scale path directly
- scale-down would bypass controller-owned disconnect, offload, and delete sequencing
- scale-up and scale-down precedence would be split between lifecycle logic and autoscaler logic
- GitOps ownership around replicas would become less explainable
- KEDA's `0 -> 1` activation and HPA's `1 -> N` scaling behavior are designed to own the target scale path directly, which conflicts with this project’s controller-owned lifecycle model

Recommendation:

- rejected for this architecture

## Recommended Smallest Clean Model

The smallest clean KEDA model is:

1. keep the built-in controller-owned autoscaler as the primary supported model
2. make KEDA optional and disabled by default
3. let KEDA write only external replica intent into the `NiFiCluster` `/scale` surface
4. let the controller translate that into a normal autoscaling recommendation and, in enforced mode, a normal controller-owned scale-up step
5. keep scale-down fully controller-native and independent of KEDA

This is intentionally narrower than generic "KEDA support". It treats KEDA as a trigger ecosystem and intent writer, not as the lifecycle executor.

## Implemented Scope

Implemented in this slice:

- an optional `NiFiCluster` `/scale` surface backed by `spec.autoscaling.external.requestedReplicas`
- controller status that reports external KEDA intent separately from the controller’s own recommendation and execution state
- optional `charts/nifi-platform` rendering for a KEDA `ScaledObject` that targets `NiFiCluster`, not the NiFi `StatefulSet`
- a generated HPA shape that disables scale-down behavior so the KEDA path stays aligned with controller-owned scale-down rules
- current Helm validation keeps the runtime path narrow by requiring managed mode plus enforced controller scale-up
- fail-fast Helm validation for invalid combinations such as disabled autoscaling, missing triggers, or KEDA enablement outside managed mode
- a focused live kind gate, `make kind-keda-scale-up-fast-e2e`, that installs real KEDA and proves the runtime contract end to end

Not implemented in this slice:

- direct `ScaledObject` targeting of the NiFi `StatefulSet`
- KEDA-owned scale-down execution
- any KEDA support in `charts/nifi`
- any new lifecycle precedence rules
- any new product CRD beyond the existing `NiFiCluster` CRD

## What KEDA Does

In the implemented experimental path, KEDA:

- observe external triggers
- compute optional scale-up intent
- publish that intent into a controller-owned surface

KEDA does not:

- mutate the NiFi `StatefulSet` directly
- bypass rollout, hibernation, restore, TLS restart, degraded-state, or autoscaling precedence rules
- decide whether a scale-down is safe
- own disconnect, offload, ordinal selection, or PVC safety

## Runtime Proof

The focused live kind gate now proves all of the following in one managed-platform run:

- KEDA installs and becomes ready on kind
- the platform chart renders and applies a `ScaledObject` only in `charts/nifi-platform`
- the rendered `ScaledObject` and generated HPA target `NiFiCluster`, not the NiFi `StatefulSet`
- KEDA writes scale-up intent through the `NiFiCluster` `/scale` surface
- the controller observes that external intent and performs the real bounded `StatefulSet` scale-up itself
- the NiFi cluster settles healthy at the larger size afterward
- unsupported external scale-down intent is still ignored and does not reduce the `StatefulSet`

That last point is intentionally narrow:

- the runtime gate exercises ignored unsupported external scale-down intent through the controller-owned external intent field
- it does not claim that KEDA or the generated HPA can or should execute scale-down
- KEDA-enabled scale-down remains unsupported in this project

## GitOps Note

When KEDA is enabled, `spec.autoscaling.external.requestedReplicas` becomes a runtime-managed field written through the Kubernetes `/scale` path. GitOps tooling should ignore drift on that field or treat it as controller/KEDA-owned runtime intent, not as a hand-authored desired-state field.

All other lifecycle ownership remains unchanged:

- `spec.desiredState` still controls hibernation and restore intent
- `spec.autoscaling.mode`, min and max bounds, and controller-native scale-down policy remain declarative user intent
- `StatefulSet.spec.replicas` remains controller-owned execution output

## Support Level

Current support level:

- KEDA is implemented as an optional experimental integration
- the built-in controller-owned autoscaler remains the primary and recommended model
- the shipped KEDA example targets `NiFiCluster`, not the NiFi `StatefulSet`
- the focused kind runtime proof is green, but the feature remains experimental rather than broadly production-recommended

## Revisit Criteria

Keep the KEDA path narrow until all of the following are true:

- external intent behaves clearly under real GitOps reconciler policies
- the controller-owned external intent status remains easy to understand in production
- KEDA-triggered scale-up proves useful enough to justify keeping the dependency optional but supported
- no pressure emerges to broaden KEDA into direct scale-down or direct `StatefulSet` ownership
