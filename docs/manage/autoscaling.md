# Autoscaling

NiFi-Fabric uses a controller-owned autoscaling model.

## What This Feature Does

The `NiFiCluster` controller is the only executor of replica changes.

Autoscaling runs in three modes:

- `Disabled`
- `Advisory`
- `Enforced`

The controller remains responsible for:

- scale-up execution
- scale-down safety checks
- disconnect, offload, and delete sequencing
- lifecycle precedence around rollout, TLS actions, hibernation, restore, degraded states, and active destructive work

## Standard Configuration Surface

Use `NiFiCluster.spec.autoscaling` or platform chart values under:

- `cluster.autoscaling.mode`
- `cluster.autoscaling.scaleUp.*`
- `cluster.autoscaling.scaleDown.*`
- `cluster.autoscaling.minReplicas`
- `cluster.autoscaling.maxReplicas`
- `cluster.autoscaling.signals`

## Support Level

- advisory autoscaling: production-ready bounded controller-owned recommendation path
- enforced scale-up: production-ready bounded controller-owned execution path
- enforced scale-down: production-ready for the bounded controller-owned sequential one-node path, including bounded sequential multi-step episodes
- KEDA integration: optional, experimental input path only

The current production-ready bounded model includes:

- confidence-based scale-up using the small explainable signal set
- bounded capacity reasoning in operator-facing recommendation messages
- actual StatefulSet removal-candidate qualification from live pod state
- bounded sequential multi-step scale-down with fresh requalification between every one-node step

## Scale-Down Position

Scale-down remains one-node-at-a-time.

That is intentional:

- NiFi node removal is destructive
- the actual StatefulSet `N -> N-1` removal pod is the only bounded one-step removal candidate in this model
- disconnect and offload must complete before deletion
- bounded multi-node removal is allowed only as a sequential controller-owned episode of one-node steps

Low-pressure evidence is also intentionally conservative.

Scale-down is only eligible when the controller has durable evidence that the cluster is genuinely quiet:

- root-process-group backlog must stay at zero across repeated evaluations
- when NiFi thread counts are available, active timer-driven work must also stay below the low-pressure threshold
- when byte backlog or thread evidence is missing, the controller requires extra consecutive qualifying samples before any removal step
- stabilization and cooldown windows still apply even after low pressure qualifies

This keeps the policy explainable:

- scale-down is allowed only after repeated trustworthy low-pressure evidence
- scale-down is blocked explicitly when zero backlog appears transient or executor activity is still too busy to trust that quiet sample
- recommendation messages now say what the bounded next step is expected to achieve, such as adding executor or CPU headroom for scale-up, or confirming that one fewer node should still fit within the current low-pressure envelope

The supported execution model is explicit:

- the controller remains the only executor
- the controller removes one node at a time
- the controller now explicitly qualifies the actual StatefulSet removal pod from live pod state before starting destructive work
- missing, terminating, or not-Ready removal candidates are rejected explicitly and block scale-down
- lower ordinals are rejected explicitly because one-step StatefulSet scale-down cannot safely widen deletion to a different pod
- the controller sequences disconnect, offload, and delete work before the replica reduction is considered settled
- `spec.autoscaling.scaleDown.maxSequentialSteps` bounds how many one-node removals may happen in one autoscaling episode
- every next removal in that episode still requires fresh low-pressure qualification, fresh candidate selection, fresh settle, and a fresh cooldown or stabilization check before the controller proceeds
- active episode progress is operator-visible through `status.autoscaling.execution.plannedSteps`, `status.autoscaling.execution.completedSteps`, and the execution or decision messages
- rollout, TLS, hibernation, restore, degraded states, and other higher-precedence destructive work can pause or block autoscaling

Future work remains separate from the support claim:

- smarter drainability selection
- broader bulk policy depth beyond the bounded sequential episode model
- broader KEDA maturity

Why broader bulk policy depth is still deferred:

- the current safety model depends on observing one destructive step at a time and then waiting for disconnect, offload, deletion, and post-removal settle work to finish cleanly
- removing more than one node without re-evaluating cluster health, backlog, and lifecycle precedence after each step would weaken the current safety contract
- the current controller intentionally does not batch disconnect or offload work across multiple pods and does not claim scheduler-like capacity planning

If future bulk policy work is ever added, it must stay bounded:

- one-step remains the default and primary supported policy
- any larger recommendation would still have to execute as a sequence of controller-owned one-node steps, not concurrent node removal
- each next step would need fresh low-pressure qualification, healthy settle, and no higher-precedence lifecycle conflict before continuing
- any degraded state, blocked execution, unresolved candidate, or conflicting external floor would stop the sequence immediately

Why this has not grown into smarter candidate selection yet:

- with a StatefulSet, one-step scale-down still removes only the actual `N -> N-1` removal pod, so choosing a lower ordinal would require broader lifecycle behavior than this model allows
- the current autoscaling evidence is intentionally bounded to live pod state plus the existing node-preparation pipeline; it does not claim trustworthy per-node queue-placement or drainability scoring
- the current support position prefers explicit blocked-or-drainable reasoning on the actual removable candidate over hidden candidate-ranking logic

## Reading Autoscaling State

Operators should read autoscaling from the existing `NiFiCluster` surfaces together:

- `spec.autoscaling.mode` shows whether the cluster is in `Disabled`, `Advisory`, or `Enforced` mode.
- `status.autoscaling.external.requestedReplicas` shows the latest external request the controller observed.
- `status.autoscaling.external.boundedReplicas` shows the controller-bounded external intent after autoscaling min and max checks.
- `status.autoscaling.recommendedReplicas` shows the controller's bounded recommendation after policy evaluation.
- `status.autoscaling.execution.phase`, `state`, `plannedSteps`, `completedSteps`, `blockedReason`, `failureReason`, and `message` show the live execution checkpoint when a scale action is settling or blocked.
- `status.autoscaling.lastScalingDecision` is the operator-facing summary for allowed, blocked, deferred, ignored, or failed decisions and now appends compact context for mode, current replicas, recommendation, request, and active execution.
- `status.nodeOperation` identifies the pod and stage being disconnected or offloaded during safe scale-down.

Typical operator interpretation:

- if `external.requestedReplicas` differs from `recommendedReplicas`, policy or safety checks have bounded or ignored the external request
- if `external.boundedReplicas` differs from `external.requestedReplicas`, the controller has already bounded the raw external request before execution is considered
- if `recommendedReplicas` differs from the current desired size but `execution` is empty, the controller is still blocked or deferred by cooldown, stabilization, lifecycle precedence, or availability gates
- if `execution.state=Blocked`, the controller will either resume automatically on the next safe reconcile or the message will tell you what operator action is needed
- when scale-down is blocked during candidate qualification, `blockedReason` and `lastScalingDecision` now also say whether the actual removal pod is missing, terminating, or not Ready and why lower ordinals were rejected
- when a sequential episode is active, the execution fields also show how many one-node removals were planned in the current episode and how many have already completed

## Optional KEDA Integration

KEDA is optional and experimental.

Built-in controller-owned autoscaling remains the primary and recommended model.

KEDA does not scale the NiFi `StatefulSet` directly. It writes external intent through `NiFiCluster`, and the controller decides whether that intent becomes a real scale action.

When KEDA is enabled:

- the generated `ScaledObject` targets `NiFiCluster`, not the NiFi `StatefulSet`
- `spec.autoscaling.external.requestedReplicas` is a runtime-managed `/scale` field and should stay at `0` in declarative Helm values
- the controller may bound, defer, or ignore that external request based on the existing autoscaling and lifecycle safety rules
- `status.autoscaling.external.reason` and `status.autoscaling.external.message` now say whether the request is actionable now, deferred by cooldown or low pressure, blocked by lifecycle precedence, or ignored
- optional external downscale intent is still best-effort controller input only, not direct execution

See [Experimental Features](../experimental-features.md) and [KEDA Integration Position](../keda.md).
