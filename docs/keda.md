# KEDA Integration Position

## Decision

KEDA external scale-up and controller-mediated external downscale intent are GA within this project's bounded autoscaling model.

KEDA remains optional and secondary to the built-in controller-owned autoscaler.

External downscale remains opt-in and bounded, but it is now part of the GA claim as controller-mediated safe scale-down intent, not as direct external replica ownership.

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
5. allow external downscale intent only as optional best-effort controller input, never as direct execution

This is intentionally narrower than generic "KEDA support". It treats KEDA as a trigger ecosystem and intent writer, not as the lifecycle executor.

External downscale intent follows the same rule:

- it stays optional and bounded
- it must be explicitly enabled on the controller-owned external intent contract
- even when enabled, it is only best-effort controller input
- the existing safe scale-down path still decides whether one highest-ordinal node may be removed

## Implemented Scope

Implemented in this slice:

- an optional `NiFiCluster` `/scale` surface backed by `spec.autoscaling.external.requestedReplicas`
- controller status that reports external KEDA intent separately from the controller’s own recommendation and execution state
- controller status that reports not only the raw external KEDA request but also the controller's current handling stage for that request, such as bounded scale-up, bounded best-effort downscale, ignored downscale, lifecycle-precedence blocking, low-pressure waiting, or cooldown waiting
- optional `charts/nifi-platform` rendering for a KEDA `ScaledObject` that targets `NiFiCluster`, not the NiFi `StatefulSet`
- a generated HPA shape that disables scale-down by default and only allows lower external `/scale` intent when `spec.autoscaling.external.scaleDownEnabled=true`
- current Helm validation keeps the runtime path narrow by requiring managed mode plus enforced controller scale-up
- fail-fast Helm validation for invalid combinations such as disabled autoscaling, missing triggers, KEDA bounds outside the controller-owned autoscaling bounds, declarative non-zero `spec.autoscaling.external.requestedReplicas`, or KEDA enablement outside managed mode
- a focused live kind gate, `make kind-keda-scale-up-fast-e2e`, that installs real KEDA and proves the scale-up runtime contract end to end
- a focused live kind gate, `make kind-keda-scale-down-fast-e2e`, that proves opt-in external downscale intent still flows only through the existing safe one-step controller pipeline and that ignored below-min downscale intent stays explicit in controller status

Not implemented in this slice:

- direct `ScaledObject` targeting of the NiFi `StatefulSet`
- KEDA-owned scale-down execution
- any KEDA support in `charts/nifi`
- any new lifecycle precedence rules
- any new product CRD beyond the existing `NiFiCluster` CRD

## What KEDA Does

In the implemented supported path, KEDA:

- observe external triggers
- compute optional scale-up intent
- compute optional best-effort downscale intent only when explicitly enabled
- publish that intent into a controller-owned surface

KEDA does not:

- mutate the NiFi `StatefulSet` directly
- bypass rollout, hibernation, restore, TLS restart, degraded-state, or autoscaling precedence rules
- decide whether a scale-down is safe
- own disconnect, offload, ordinal selection, or PVC safety
- bypass a matching or higher external requested replica floor with an internal low-pressure downscale

## Runtime Proof

The focused live kind gate now proves all of the following in one managed-platform run:

- KEDA installs and becomes ready on kind
- the platform chart renders and applies a `ScaledObject` only in `charts/nifi-platform`
- the rendered `ScaledObject` and generated HPA target `NiFiCluster`, not the NiFi `StatefulSet`
- KEDA writes scale-up intent through the `NiFiCluster` `/scale` surface
- the controller observes that external intent and performs the real bounded `StatefulSet` scale-up itself
- the NiFi cluster settles healthy at the larger size afterward
- when external downscale is enabled, KEDA lowers `NiFiCluster` `/scale` back to `minReplicaCount`
- the controller then performs one safe highest-ordinal `3 -> 2` downscale step itself and settles cleanly when the existing safe checks are satisfied
- unsupported or out-of-policy external scale-down intent is still ignored and does not reduce the `StatefulSet`

The external downscale proof remains intentionally bounded:

- it proves only one-step controller-mediated scale-down through the existing safe path
- it does not claim that KEDA or the generated HPA can or should execute scale-down directly
- external downscale remains bounded in this project even as the controller-mediated path is now GA

Focused repo tests now strengthen the support contract around the runtime gates:

- KEDA external intent remains visible and explicitly blocked while rollout, TLS observation or restart work, restore, hibernation, degraded state, or already-running destructive autoscaling work has precedence
- the blocked status message now identifies the winning controller-owned activity instead of looking like generic deferral
- controller restart does not lose the runtime-managed KEDA request; the next reconcile rebuilds status from the persisted `/scale` input and only converges after the blocking condition clears
- KEDA scale-up and KEDA downscale are intentionally treated differently after conflicts clear: scale-up can resume through the normal one-step controller path, while downscale still must re-qualify low pressure, stabilization, cooldown, and safe node-removal checks before any node is removed

## GitOps Note

When KEDA is enabled, `spec.autoscaling.external.requestedReplicas` becomes a runtime-managed field written through the Kubernetes `/scale` path. GitOps tooling should ignore drift on that field or treat it as controller/KEDA-owned runtime intent, not as a hand-authored desired-state field.

The platform chart now reinforces that contract by requiring declarative values to leave `cluster.autoscaling.external.requestedReplicas=0` when `keda.enabled=true`.

All other lifecycle ownership remains unchanged:

- `spec.desiredState` still controls hibernation and restore intent
- `spec.autoscaling.mode`, min and max bounds, and controller-native scale-down policy remain declarative user intent
- `StatefulSet.spec.replicas` remains controller-owned execution output

## What Operators Should Expect

When KEDA is enabled, operators should expect the following:

- KEDA may publish a new external request through `NiFiCluster` `/scale`, but that does not mean the controller will execute the same size immediately
- the controller may bound the raw request to the configured autoscaling min and max before any execution is considered
- the controller may defer that bounded request because rollout, TLS restart, hibernation, restore, degraded state, cooldown, or low-pressure rules still take precedence
- the controller may ignore a downscale request when external downscale is disabled or when the request is already at or below the allowed floor
- when controller-owned lifecycle or destructive work has precedence, `status.autoscaling.external.reason=ExternalIntentBlocked` and the message names the winning activity
- when no higher-precedence work is active but scale-up cooldown, low-pressure evidence, stabilization, or scale-down cooldown still need time, the controller reports the request as deferred rather than blocked
- controller events now separate blocked, deferred, and ignored external intent so operators can distinguish lifecycle precedence from ordinary waiting
- controller restart does not clear the KEDA request because the `/scale` input remains persisted on `NiFiCluster`; the controller rebuilds the handling state on the next reconcile
- clearing a lifecycle conflict does not grant KEDA direct authority over scale-down; the controller still rechecks the existing safe one-node scale-down path before any pod removal
- the controller remains the source of truth for whether a real `StatefulSet` scale action started, is blocked, or was intentionally skipped

The main operator-facing fields are:

- `spec.autoscaling.external.requestedReplicas` for the runtime-managed request currently written through `/scale`
- `status.autoscaling.external.*` for the request the controller observed, any controller bounds, and whether that request is actionable, deferred, blocked, or ignored
- `status.autoscaling.execution.*` for live controller execution state when a real scale action is in progress or blocked
- `status.autoscaling.lastScalingDecision` for the operator-facing summary of what KEDA wanted and what the controller decided to do

## Operational Support Package

The shipped starter operations package now treats KEDA as a supportable bounded feature:

- [Operations and Troubleshooting](operations.md) includes KEDA quick checks and controller-status interpretation
- [Operations Alerts](operations/alerts.md) explains guidance-first KEDA alerting for ignored, blocked, deferred, and GitOps-conflicted intent
- [Operations Runbooks](operations/runbooks.md) includes dedicated KEDA runbooks for received-versus-applied intent, downscale refusal, lifecycle blocking, and GitOps conflict
- [Verification and Support Levels](testing.md) distinguishes the focused live KEDA gates from the repo-test proof for lifecycle conflict and restart-safe behavior

## Support Level

Current support level:

- GA: KEDA external scale-up intent through `NiFiCluster` `/scale`
- GA: controller-mediated KEDA external downscale intent through the same runtime-managed surface and bounded safe scale-down policy
- the built-in controller-owned autoscaler remains the primary and recommended model
- the shipped KEDA example targets `NiFiCluster`, not the NiFi `StatefulSet`
- the focused kind runtime proof is green, and the starter operations package now documents how to support received, ignored, blocked, deferred, and GitOps-conflicted external intent

## Support Boundary

Keep the KEDA path narrow even as it becomes supportable:

- do not broaden KEDA into direct `StatefulSet` ownership
- do not add a second autoscaling control plane
- do not bypass rollout, TLS, hibernation, restore, degraded-state, or safe scale-down precedence
- do not treat opt-in external downscale as direct KEDA scale authority; it remains bounded controller-mediated input only
- do not expect generic KEDA support outside the bounded `NiFiCluster` external-intent model
