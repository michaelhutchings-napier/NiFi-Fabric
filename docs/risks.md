# Risks

## Top Architecture Risks

### Boundary Creep

Risk:

- the controller slowly absorbs more deployment configuration and starts to duplicate Helm

Mitigation:

- keep one CRD in MVP
- require an ADR and `docs/api.md` update for any new CRD or major field expansion
- reject feature additions that NiFi 2 already handles natively

### GitOps Ownership Confusion

Risk:

- GitOps tooling and the controller fight over the same fields

Mitigation:

- document exact controller-owned mutations
- keep controller ownership narrow
- require ignore-difference on `StatefulSet.spec.replicas` only for managed hibernation

## Complexity Risks

### Rollout Logic Becomes Fragile

Risk:

- restart sequencing grows complicated and hard to debug

Mitigation:

- use `OnDelete` and simple one-pod-at-a-time orchestration
- keep explicit status conditions and last-operation state
- test restart, timeout, and resume behavior with `envtest` and kind

### Cert Rotation Behavior Is Surprising

Risk:

- users do not know whether TLS drift will autoreload or restart

Mitigation:

- document a policy-driven strategy
- default to autoreload-first only when refs, paths, and passwords are stable
- surface the chosen action in status and events

## Operational Risks

### Controller To NiFi Authentication

Risk:

- the controller may fail to call secured NiFi APIs reliably

Mitigation:

- use a dedicated management identity
- keep stable TLS mount paths
- test expired, rotated, and mismatched credentials explicitly

### Hibernation Restore Mismatch

Risk:

- unhibernate restores the wrong replica count

Mitigation:

- persist `status.hibernation.lastRunningReplicas`
- record fallback behavior when the field is absent
- test controller restart during hibernation and restore

### Unsafe Autoscaling Path

Risk:

- an autoscaler writes directly to `StatefulSet.spec.replicas` and bypasses NiFi disconnect and offload requirements

Mitigation:

- keep one lifecycle control plane
- treat scale-down as a managed destructive operation, not a plain replica update
- prefer advisory autoscaling first
- require any enforced scaling to flow through the existing `NiFiCluster` controller plane

### KEDA Split-Control-Plane Risk

Risk:

- KEDA or its generated HPA becomes a second scaling control plane, either by mutating the `StatefulSet` directly or by introducing a controller-unclear desired replica contract

Mitigation:

- keep KEDA optional and narrow even now that a controller-owned external intent contract exists
- reject direct `ScaledObject` targeting of the NiFi `StatefulSet`
- limit the current slice to optional scale-up intent only and keep scale-down controller-native
- keep GitOps ownership explicit for the external intent surface
- require GitOps tooling to ignore or accept drift on `spec.autoscaling.external.requestedReplicas` when KEDA is enabled

### Signal Quality Mismatch

Risk:

- CPU or memory based autoscaling reacts to host pressure while missing queue backlog, stuck flow state, or repository pressure

Mitigation:

- prefer NiFi-native signals first
- use CPU only as a secondary signal
- prove signal quality in advisory mode before allowing automatic replica changes
- keep low-pressure scale-down conservative until sustained queue-age evidence is available reliably enough from NiFi runtime APIs
- require repeated zero-backlog observations before the existing stabilization window can start
- persist autoscaling execution state, blocked reasons, and failure reasons so restart recovery does not repeat destructive steps while signal quality is still intentionally narrow

### Hibernation And Autoscaling Conflict

Risk:

- autoscaling intent and hibernation or restore both try to control replica count and produce oscillation or surprising restores

Mitigation:

- define one precedence model before implementation
- suspend enforced autoscaling during hibernation and restore
- persist autoscaling intent separately from the last running restore baseline
- document GitOps ownership explicitly: external autoscalers do not mutate the `StatefulSet` directly in this slice

### Stuck Offload And Silent Retry Risk

Risk:

- a timed-out or repeatedly failing NiFi offload path silently retries and obscures whether autoscaling is safely blocked or still making progress

Mitigation:

- keep `status.nodeOperation` persisted while autoscaling scale-down is blocked in prepare
- surface blocked and failed autoscaling execution state explicitly in `status.autoscaling.execution`
- emit events and metrics when autoscaling execution blocks, resumes, or fails
- keep scale-down one-step-at-a-time so recovery scope stays local to the highest ordinal node

## Upgrade Risks

### NiFi 2 Minor Version Differences

Risk:

- Kubernetes coordination or TLS behavior may differ across NiFi 2.x releases

Mitigation:

- publish a supported version matrix
- pin tested versions in CI
- keep upgrade logic conservative and health-gated

## Compatibility Risks

### OpenShift Differences

Risk:

- OpenShift SCC, Routes, and defaults can diverge from AKS assumptions

Mitigation:

- treat OpenShift as friendly second target, not equal MVP scope
- document required adjustments
- add compatibility checks after AKS-first behavior is stable
