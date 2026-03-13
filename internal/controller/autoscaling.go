package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const (
	autoscalingReasonDisabled           = "Disabled"
	autoscalingReasonTargetNotResolved  = "TargetNotResolved"
	autoscalingReasonUnmanagedTarget    = "UnmanagedTarget"
	autoscalingReasonHibernated         = "Hibernated"
	autoscalingReasonProgressing        = "Progressing"
	autoscalingReasonDegraded           = "Degraded"
	autoscalingReasonUnavailable        = "Unavailable"
	autoscalingReasonBelowMinReplicas   = "BelowMinReplicas"
	autoscalingReasonAboveMaxReplicas   = "AboveMaxReplicas"
	autoscalingReasonQueuePressure      = "QueuePressureDetected"
	autoscalingReasonCPUSaturation      = "CPUSaturationDetected"
	autoscalingReasonLowPressure        = "LowPressureDetected"
	autoscalingReasonMaxReplicasReached = "MaxReplicasReached"
	autoscalingReasonNoActionableInput  = "NoActionableSignals"

	defaultAutoscalingScaleUpCooldown        = 5 * time.Minute
	defaultAutoscalingScaleDownCooldown      = 10 * time.Minute
	defaultAutoscalingScaleDownStabilization = 5 * time.Minute
	defaultAutoscalingLowPressureSamples     = 3
)

func (r *NiFiClusterReconciler) syncAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) {
	persisted := r.latestAutoscalingStatus(ctx, cluster)
	desired, samples := r.buildAutoscalingStatus(ctx, cluster)
	if autoscalingStatusEqual(cluster.Status.Autoscaling, desired) {
		if persisted.LastEvaluationTime != nil {
			desired.LastEvaluationTime = persisted.LastEvaluationTime.DeepCopy()
		}
	} else {
		now := metav1.NewTime(time.Now().UTC())
		desired.LastEvaluationTime = &now
	}
	desired.LowPressure = updatedAutoscalingLowPressureStatus(persisted, desired, samples)
	desired.LowPressureSince = desired.LowPressure.Since
	if persisted.LastScaleUpTime != nil {
		desired.LastScaleUpTime = persisted.LastScaleUpTime.DeepCopy()
	}
	if persisted.LastScaleDownTime != nil {
		desired.LastScaleDownTime = persisted.LastScaleDownTime.DeepCopy()
	}
	desired.Execution = mergeAutoscalingExecutionStatus(cluster.Status.Autoscaling.Execution, persisted.Execution)
	if autoscalingExecutionShouldClear(cluster, desired.Execution) {
		desired.Execution = platformv1alpha1.AutoscalingExecutionStatus{}
	}
	desired.LastScalingDecision = persisted.LastScalingDecision
	if shouldRefreshAutoscalingDecision(cluster, desired.Execution, desired.LastScalingDecision) {
		desired.LastScalingDecision = autoscalingNoScaleDecision(cluster, desired)
	}
	cluster.Status.Autoscaling = desired
	recordAutoscalingSignalSamples(cluster, samples)
}

func (r *NiFiClusterReconciler) latestAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) platformv1alpha1.AutoscalingStatus {
	status := cluster.Status.Autoscaling
	if r.APIReader == nil {
		return status
	}

	latest := &platformv1alpha1.NiFiCluster{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(cluster), latest); err != nil {
		return status
	}
	return mergedAutoscalingStatus(status, latest.Status.Autoscaling)
}

func mergedAutoscalingStatus(current, persisted platformv1alpha1.AutoscalingStatus) platformv1alpha1.AutoscalingStatus {
	merged := current
	preservedScaleAction := false

	if merged.LastEvaluationTime == nil || (persisted.LastEvaluationTime != nil && persisted.LastEvaluationTime.After(merged.LastEvaluationTime.Time)) {
		merged.LastEvaluationTime = persisted.LastEvaluationTime.DeepCopy()
	}
	if merged.LastScaleUpTime == nil || (persisted.LastScaleUpTime != nil && persisted.LastScaleUpTime.After(merged.LastScaleUpTime.Time)) {
		merged.LastScaleUpTime = persisted.LastScaleUpTime.DeepCopy()
		preservedScaleAction = preservedScaleAction || persisted.LastScaleUpTime != nil
	}
	if merged.LastScaleDownTime == nil || (persisted.LastScaleDownTime != nil && persisted.LastScaleDownTime.After(merged.LastScaleDownTime.Time)) {
		merged.LastScaleDownTime = persisted.LastScaleDownTime.DeepCopy()
		preservedScaleAction = preservedScaleAction || persisted.LastScaleDownTime != nil
	}
	merged.LowPressure = mergeAutoscalingLowPressureStatus(current.LowPressure, persisted.LowPressure)
	if merged.LowPressureSince == nil && merged.LowPressure.Since != nil {
		merged.LowPressureSince = merged.LowPressure.Since.DeepCopy()
	}
	merged.Execution = mergeAutoscalingExecutionStatus(current.Execution, persisted.Execution)
	if merged.LastScalingDecision == "" || preservedScaleAction {
		merged.LastScalingDecision = persisted.LastScalingDecision
	}

	return merged
}

func shouldRefreshAutoscalingDecision(cluster *platformv1alpha1.NiFiCluster, execution platformv1alpha1.AutoscalingExecutionStatus, currentDecision string) bool {
	if autoscalingStatusExecutionInProgress(cluster, execution) {
		return false
	}
	if autoscalingTimedBlockStillActive(currentDecision) {
		return false
	}
	return true
}

func autoscalingNoScaleDecision(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	policy := cluster.Spec.Autoscaling
	switch autoscalingMode(policy) {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		return "NoScale: autoscaling is not in enforced mode"
	}
	if status.RecommendedReplicas == nil {
		return fmt.Sprintf("NoScale: recommendation is unavailable because %s", status.Reason)
	}

	currentReplicas := cluster.Status.Replicas.Desired
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	switch {
	case recommendedReplicas > currentReplicas:
		if !policy.ScaleUp.Enabled {
			return "NoScaleUp: scale-up is not enabled"
		}
		if status.LastScaleUpTime != nil {
			nextEligibleTime := status.LastScaleUpTime.Time.Add(autoscalingScaleUpCooldown(policy))
			if time.Now().UTC().Before(nextEligibleTime) {
				return fmt.Sprintf("NoScaleUp: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			}
		}
		return "NoScaleUp: waiting for the steady-state autoscaling executor"
	case recommendedReplicas < currentReplicas:
		if !policy.ScaleDown.Enabled {
			return "NoScaleDown: scale-down is not enabled"
		}
		if status.LowPressureSince == nil {
			return "NoScaleDown: low pressure is not currently observed"
		}
		if !autoscalingLowPressureRequirementMet(status.LowPressure) {
			return fmt.Sprintf(
				"NoScaleDown: low pressure needs %d/%d consecutive zero-backlog evaluations before any scale-down step",
				status.LowPressure.ConsecutiveSamples,
				status.LowPressure.RequiredConsecutiveSamples,
			)
		}
		nextStableTime := status.LowPressureSince.Time.Add(autoscalingScaleDownStabilizationWindow(policy))
		if time.Now().UTC().Before(nextStableTime) {
			return fmt.Sprintf("NoScaleDown: low pressure must remain stable until %s", nextStableTime.UTC().Format(time.RFC3339))
		}
		if status.LastScaleDownTime != nil {
			nextEligibleTime := status.LastScaleDownTime.Time.Add(autoscalingScaleDownCooldown(policy))
			if time.Now().UTC().Before(nextEligibleTime) {
				return fmt.Sprintf("NoScaleDown: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			}
		}
		return "NoScaleDown: waiting for the steady-state autoscaling executor"
	}
	return "NoScale: recommended replicas are already satisfied"
}

func (r *NiFiClusterReconciler) buildAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	target := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}, target); err != nil {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonTargetNotResolved}, autoscalingSignalCollection{}
	}

	return r.buildAutoscalingStatusForTarget(ctx, cluster, target)
}

func (r *NiFiClusterReconciler) buildAutoscalingStatusForTarget(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	policy := cluster.Spec.Autoscaling
	blocked, blockedReason := blockedAutoscalingStatus(cluster)
	if blocked {
		return platformv1alpha1.AutoscalingStatus{Reason: blockedReason}, autoscalingSignalCollection{}
	}
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonDisabled}, autoscalingSignalCollection{}
	}

	signals := autoscalingSignals(policy)
	collection := autoscalingSignalCollection{
		SignalStatuses: make([]platformv1alpha1.AutoscalingSignalStatus, 0, len(signals)),
	}
	if r.AutoscalingCollector != nil {
		collection = r.AutoscalingCollector.Collect(ctx, cluster, target, signals)
	}
	if len(collection.SignalStatuses) == 0 {
		collection.SignalStatuses = unavailableAutoscalingSignals(signals, "autoscaling signal collector is not configured")
	}

	return buildAutoscalingSteadyStateStatus(cluster, policy, collection), collection
}

func (r *NiFiClusterReconciler) maybeExecuteAutoscalingScaleUp(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (bool, ctrl.Result, error) {
	status, _ := r.buildAutoscalingStatusForTarget(ctx, cluster, target)
	executionState := r.autoscalingExecutionState(ctx, cluster, target)

	policy := cluster.Spec.Autoscaling
	mode := autoscalingMode(policy)
	switch mode {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScale: autoscaling is not in enforced mode"
		return false, ctrl.Result{}, nil
	}

	if !policy.ScaleUp.Enabled {
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: scale-up is not enabled"
		return false, ctrl.Result{}, nil
	}
	if status.RecommendedReplicas == nil {
		cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleUp: recommendation is unavailable because %s", status.Reason)
		return false, ctrl.Result{}, nil
	}

	currentReplicas := executionState.currentReplicas
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	switch {
	case recommendedReplicas < currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: recommended replicas require a smaller cluster"
		return false, ctrl.Result{}, nil
	case recommendedReplicas == currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScale: recommended replicas are already satisfied"
		return false, ctrl.Result{}, nil
	}

	cooldown := autoscalingScaleUpCooldown(policy)
	if executionState.lastScaleUpTime != nil && cooldown > 0 {
		nextEligibleTime := executionState.lastScaleUpTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleUp: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			return false, ctrl.Result{}, nil
		}
	}

	desiredReplicas := currentReplicas + 1
	if desiredReplicas > recommendedReplicas {
		desiredReplicas = recommendedReplicas
	}
	maxReplicas := autoscalingMaxReplicas(policy, autoscalingMinReplicas(policy, currentReplicas), currentReplicas)
	if desiredReplicas > maxReplicas {
		desiredReplicas = maxReplicas
	}
	if desiredReplicas <= currentReplicas {
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: bounded recommendation does not allow a larger target size"
		return false, ctrl.Result{}, nil
	}

	scaledTarget := target.DeepCopy()
	scaledTarget.Spec.Replicas = ptrTo(desiredReplicas)
	if err := r.Update(ctx, scaledTarget); err != nil {
		return false, ctrl.Result{}, fmt.Errorf("update StatefulSet replicas for autoscaling scale-up: %w", err)
	}

	now := metav1.NewTime(time.Now().UTC())
	decision := fmt.Sprintf("ScaleUp: increased target StatefulSet replicas from %d to %d", currentReplicas, desiredReplicas)
	cluster.Status.Autoscaling.LastScalingDecision = decision
	cluster.Status.Autoscaling.LastScaleUpTime = &now
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle, platformv1alpha1.AutoscalingExecutionStateRunning, desiredReplicas, "", "", fmt.Sprintf("Waiting for the autoscaling scale-up step to settle at %d replicas", desiredReplicas))
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleUp", fmt.Sprintf("%s because %s", decision, autoscalingStatusMessage(status)))
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "AutoscalingScaleUp",
		Message:            fmt.Sprintf("Managed autoscaling is scaling the cluster from %d to %d replicas", currentReplicas, desiredReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleUp",
		Message:            fmt.Sprintf("Waiting for the scaled cluster to become healthy at %d replicas", desiredReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleUp",
		Message:            "Autoscaling scale-up is in progress",
		LastTransitionTime: metav1.Now(),
	})
	recordAutoscalingScaleAction("scaled_up")
	if r.Recorder != nil {
		r.Recorder.Event(cluster, "Normal", "AutoscalingScaleUp", fmt.Sprintf("Managed autoscaling increased replicas from %d to %d", currentReplicas, desiredReplicas))
	}

	return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

type autoscalingExecutionState struct {
	currentReplicas   int32
	lastScaleUpTime   *metav1.Time
	lastScaleDownTime *metav1.Time
}

func (r *NiFiClusterReconciler) autoscalingExecutionState(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) autoscalingExecutionState {
	state := autoscalingExecutionState{
		currentReplicas: cluster.Status.Replicas.Desired,
	}
	if cluster.Status.Autoscaling.LastScaleUpTime != nil {
		state.lastScaleUpTime = cluster.Status.Autoscaling.LastScaleUpTime.DeepCopy()
	}
	if cluster.Status.Autoscaling.LastScaleDownTime != nil {
		state.lastScaleDownTime = cluster.Status.Autoscaling.LastScaleDownTime.DeepCopy()
	}
	if target != nil && target.Spec.Replicas != nil && *target.Spec.Replicas > state.currentReplicas {
		state.currentReplicas = *target.Spec.Replicas
	}

	if r.APIReader == nil {
		return state
	}

	latestCluster := &platformv1alpha1.NiFiCluster{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(cluster), latestCluster); err != nil {
		return state
	}
	if latestCluster.Status.Replicas.Desired > state.currentReplicas {
		state.currentReplicas = latestCluster.Status.Replicas.Desired
	}
	if latestCluster.Status.Autoscaling.LastScaleUpTime != nil {
		if state.lastScaleUpTime == nil || latestCluster.Status.Autoscaling.LastScaleUpTime.After(state.lastScaleUpTime.Time) {
			state.lastScaleUpTime = latestCluster.Status.Autoscaling.LastScaleUpTime.DeepCopy()
		}
	}
	if latestCluster.Status.Autoscaling.LastScaleDownTime != nil {
		if state.lastScaleDownTime == nil || latestCluster.Status.Autoscaling.LastScaleDownTime.After(state.lastScaleDownTime.Time) {
			state.lastScaleDownTime = latestCluster.Status.Autoscaling.LastScaleDownTime.DeepCopy()
		}
	}
	return state
}

func blockedAutoscalingStatus(cluster *platformv1alpha1.NiFiCluster) (bool, string) {
	policy := cluster.Spec.Autoscaling
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		return true, autoscalingReasonDisabled
	}

	if !conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionTargetResolved)) {
		return true, autoscalingReasonTargetNotResolved
	}

	availableCondition := cluster.GetCondition(platformv1alpha1.ConditionAvailable)
	if availableCondition != nil && availableCondition.Reason == "UnmanagedTarget" {
		return true, autoscalingReasonUnmanagedTarget
	}

	if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated || conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionHibernated)) {
		return true, autoscalingReasonHibernated
	}

	if conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionProgressing)) {
		return true, autoscalingReasonProgressing
	}

	if conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionDegraded)) {
		return true, autoscalingReasonDegraded
	}

	if !conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionAvailable)) {
		return true, autoscalingReasonUnavailable
	}

	return false, ""
}

func buildAutoscalingSteadyStateStatus(cluster *platformv1alpha1.NiFiCluster, policy platformv1alpha1.AutoscalingPolicy, collection autoscalingSignalCollection) platformv1alpha1.AutoscalingStatus {
	currentReplicas := cluster.Status.Replicas.Desired
	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	maxReplicas := autoscalingMaxReplicas(policy, minReplicas, currentReplicas)

	recommended := currentReplicas
	reason := autoscalingReasonNoActionableInput

	switch {
	case currentReplicas < minReplicas:
		recommended = minReplicas
		reason = autoscalingReasonBelowMinReplicas
	case currentReplicas > maxReplicas:
		recommended = maxReplicas
		reason = autoscalingReasonAboveMaxReplicas
	case collection.QueuePressure.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonQueuePressure
	case collection.CPU.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonCPUSaturation
	case collection.QueuePressure.LowPressure && currentReplicas > minReplicas:
		recommended = currentReplicas - 1
		reason = autoscalingReasonLowPressure
	}

	if recommended > maxReplicas {
		if reason == autoscalingReasonQueuePressure || reason == autoscalingReasonCPUSaturation {
			recommended = maxReplicas
			reason = autoscalingReasonMaxReplicasReached
		} else {
			recommended = maxReplicas
			reason = autoscalingReasonAboveMaxReplicas
		}
	}

	return platformv1alpha1.AutoscalingStatus{
		RecommendedReplicas: ptrTo(recommended),
		Reason:              reason,
		Signals:             collection.SignalStatuses,
	}
}

func autoscalingMode(policy platformv1alpha1.AutoscalingPolicy) platformv1alpha1.AutoscalingMode {
	if policy.Mode == "" {
		return platformv1alpha1.AutoscalingModeDisabled
	}
	return policy.Mode
}

func autoscalingScaleUpCooldown(policy platformv1alpha1.AutoscalingPolicy) time.Duration {
	if policy.ScaleUp.Cooldown.Duration > 0 {
		return policy.ScaleUp.Cooldown.Duration
	}
	return defaultAutoscalingScaleUpCooldown
}

func autoscalingScaleDownCooldown(policy platformv1alpha1.AutoscalingPolicy) time.Duration {
	if policy.ScaleDown.Cooldown.Duration > 0 {
		return policy.ScaleDown.Cooldown.Duration
	}
	return defaultAutoscalingScaleDownCooldown
}

func autoscalingScaleDownStabilizationWindow(policy platformv1alpha1.AutoscalingPolicy) time.Duration {
	if policy.ScaleDown.StabilizationWindow.Duration > 0 {
		return policy.ScaleDown.StabilizationWindow.Duration
	}
	return defaultAutoscalingScaleDownStabilization
}

func autoscalingSignals(policy platformv1alpha1.AutoscalingPolicy) []platformv1alpha1.AutoscalingSignal {
	if len(policy.Signals) == 0 {
		return []platformv1alpha1.AutoscalingSignal{platformv1alpha1.AutoscalingSignalQueuePressure}
	}

	signals := make([]platformv1alpha1.AutoscalingSignal, 0, len(policy.Signals))
	for _, signal := range policy.Signals {
		if signal == "" || slices.Contains(signals, signal) {
			continue
		}
		signals = append(signals, signal)
	}
	if len(signals) == 0 {
		return []platformv1alpha1.AutoscalingSignal{platformv1alpha1.AutoscalingSignalQueuePressure}
	}
	return signals
}

func unavailableAutoscalingSignals(signals []platformv1alpha1.AutoscalingSignal, message string) []platformv1alpha1.AutoscalingSignalStatus {
	statuses := make([]platformv1alpha1.AutoscalingSignalStatus, 0, len(signals))
	for _, signal := range signals {
		statuses = append(statuses, platformv1alpha1.AutoscalingSignalStatus{
			Type:      signal,
			Available: false,
			Message:   message,
		})
	}
	return statuses
}

func autoscalingMinReplicas(policy platformv1alpha1.AutoscalingPolicy, current int32) int32 {
	if policy.MinReplicas > 0 {
		return policy.MinReplicas
	}
	if current > 0 {
		return current
	}
	return 1
}

func autoscalingMaxReplicas(policy platformv1alpha1.AutoscalingPolicy, minReplicas, current int32) int32 {
	maxReplicas := policy.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = current
	}
	if maxReplicas < minReplicas {
		return minReplicas
	}
	return maxReplicas
}

func autoscalingStatusEqual(left, right platformv1alpha1.AutoscalingStatus) bool {
	if left.Reason != right.Reason {
		return false
	}
	if !equalOptionalInt32(left.RecommendedReplicas, right.RecommendedReplicas) {
		return false
	}
	if !equalOptionalTime(left.LowPressureSince, right.LowPressureSince) {
		return false
	}
	if !autoscalingLowPressureMeaningEqual(left.LowPressure, right.LowPressure) {
		return false
	}
	if len(left.Signals) != len(right.Signals) {
		return false
	}
	for i := range left.Signals {
		if left.Signals[i] != right.Signals[i] {
			return false
		}
	}
	return true
}

func autoscalingStatusMeaningEqual(left, right platformv1alpha1.AutoscalingStatus) bool {
	if left.Reason != right.Reason {
		return false
	}
	if !equalOptionalInt32(left.RecommendedReplicas, right.RecommendedReplicas) {
		return false
	}
	if !equalOptionalTime(left.LowPressureSince, right.LowPressureSince) {
		return false
	}
	if !autoscalingLowPressureMeaningEqual(left.LowPressure, right.LowPressure) {
		return false
	}
	if len(left.Signals) != len(right.Signals) {
		return false
	}
	for i := range left.Signals {
		if left.Signals[i].Type != right.Signals[i].Type || left.Signals[i].Available != right.Signals[i].Available {
			return false
		}
	}
	return true
}

func autoscalingLowPressureMeaningEqual(left, right platformv1alpha1.AutoscalingLowPressureStatus) bool {
	if !equalOptionalTime(left.Since, right.Since) {
		return false
	}
	if left.ConsecutiveSamples != right.ConsecutiveSamples {
		return false
	}
	if left.RequiredConsecutiveSamples != right.RequiredConsecutiveSamples {
		return false
	}
	if left.FlowFilesQueued != right.FlowFilesQueued {
		return false
	}
	if left.BytesQueued != right.BytesQueued {
		return false
	}
	return left.BytesQueuedObserved == right.BytesQueuedObserved
}

func equalOptionalInt32(left, right *int32) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

func equalOptionalTime(left, right *metav1.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(right)
	}
}

func autoscalingStatusMessage(status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return "Autoscaling is disabled"
	case autoscalingReasonTargetNotResolved:
		return "Autoscaling is waiting for the target StatefulSet to resolve"
	case autoscalingReasonUnmanagedTarget:
		return "Autoscaling is blocked because the target StatefulSet is unmanaged"
	case autoscalingReasonHibernated:
		return "Autoscaling is blocked while the cluster is hibernated or restoring"
	case autoscalingReasonProgressing:
		return "Autoscaling is blocked while managed lifecycle work is in progress"
	case autoscalingReasonDegraded:
		return "Autoscaling is blocked while the cluster is degraded"
	case autoscalingReasonUnavailable:
		return "Autoscaling is blocked until the cluster is available"
	case autoscalingReasonBelowMinReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is below the configured minimum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonAboveMaxReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is above the configured maximum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonQueuePressure:
		return fmt.Sprintf("Autoscaling recommends %d replicas because root process-group backlog is present while NiFi timer-driven threads are saturated. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonCPUSaturation:
		return fmt.Sprintf("Autoscaling recommends %d replicas because NiFi system diagnostics report CPU saturation. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonLowPressure:
		return fmt.Sprintf("Autoscaling recommends %d replicas because NiFi root process-group backlog is repeatedly zero and no higher-priority scale-up pressure is active. Low-pressure evidence: %s. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), emptyIfUnset(status.LowPressure.Message, "none"), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonMaxReplicasReached:
		return fmt.Sprintf("Autoscaling is observing scale-up pressure, but the recommendation remains at %d replicas because the configured maximum has already been reached. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	default:
		summary := summarizeAutoscalingSignals(status.Signals)
		if summary == "" {
			return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently available", derefOptionalInt32(status.RecommendedReplicas))
		}
		return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently driving a scale-up. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summary)
	}
}

func autoscalingStatusOutcome(status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return "disabled"
	case autoscalingReasonBelowMinReplicas:
		return "increase"
	case autoscalingReasonAboveMaxReplicas, autoscalingReasonLowPressure:
		return "decrease"
	case autoscalingReasonQueuePressure, autoscalingReasonCPUSaturation:
		return "increase"
	case autoscalingReasonMaxReplicasReached, autoscalingReasonNoActionableInput:
		return "hold"
	default:
		return "blocked"
	}
}

func derefOptionalInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func autoscalingExecutionInProgress(cluster *platformv1alpha1.NiFiCluster) bool {
	return autoscalingStatusExecutionInProgress(cluster, cluster.Status.Autoscaling.Execution)
}

func autoscalingStatusExecutionInProgress(cluster *platformv1alpha1.NiFiCluster, execution platformv1alpha1.AutoscalingExecutionStatus) bool {
	if execution.Phase != "" {
		return execution.State != platformv1alpha1.AutoscalingExecutionStateFailed
	}

	decision := cluster.Status.Autoscaling.LastScalingDecision
	if strings.HasPrefix(decision, "ScaleUp:") || strings.HasPrefix(decision, "ScaleDown:") {
		return true
	}

	condition := cluster.GetCondition(platformv1alpha1.ConditionProgressing)
	return conditionIsTrue(condition) && (condition.Reason == "AutoscalingScaleUp" ||
		condition.Reason == "AutoscalingScaleDown" ||
		condition.Reason == "PreparingNodeForScaleDown" ||
		condition.Reason == "WaitingForAutoscalingScaleDown")
}

func mergeAutoscalingExecutionStatus(current, persisted platformv1alpha1.AutoscalingExecutionStatus) platformv1alpha1.AutoscalingExecutionStatus {
	merged := current

	if merged.Phase == "" {
		merged.Phase = persisted.Phase
	}
	if merged.Phase == "" {
		return platformv1alpha1.AutoscalingExecutionStatus{}
	}
	if merged.State == "" {
		merged.State = persisted.State
		if merged.State == "" {
			merged.State = platformv1alpha1.AutoscalingExecutionStateRunning
		}
	}
	if merged.Phase == persisted.Phase {
		if merged.StartedAt == nil || (persisted.StartedAt != nil && persisted.StartedAt.After(merged.StartedAt.Time)) {
			merged.StartedAt = persisted.StartedAt.DeepCopy()
		}
		if merged.LastTransitionTime == nil || (persisted.LastTransitionTime != nil && persisted.LastTransitionTime.After(merged.LastTransitionTime.Time)) {
			merged.LastTransitionTime = persisted.LastTransitionTime.DeepCopy()
		}
		if merged.TargetReplicas == nil && persisted.TargetReplicas != nil {
			merged.TargetReplicas = ptrTo(*persisted.TargetReplicas)
		}
		if merged.Message == "" {
			merged.Message = persisted.Message
		}
		if merged.BlockedReason == "" {
			merged.BlockedReason = persisted.BlockedReason
		}
		if merged.FailureReason == "" {
			merged.FailureReason = persisted.FailureReason
		}
		return merged
	}
	if merged.StartedAt == nil {
		now := metav1.NewTime(time.Now().UTC())
		merged.StartedAt = &now
	}
	if merged.LastTransitionTime == nil {
		now := metav1.NewTime(time.Now().UTC())
		merged.LastTransitionTime = &now
	}
	return merged
}

func autoscalingExecutionShouldClear(cluster *platformv1alpha1.NiFiCluster, execution platformv1alpha1.AutoscalingExecutionStatus) bool {
	if execution.Phase == "" {
		return false
	}
	if execution.State == platformv1alpha1.AutoscalingExecutionStateFailed {
		return false
	}
	progressing := conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionProgressing))
	available := conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionAvailable))

	switch execution.Phase {
	case platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle:
		return !progressing && available
	case platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle:
		return !autoscalingScaleDownLifecycleInProgress(cluster) && !progressing
	default:
		return false
	}
}

func setAutoscalingExecutionStatus(cluster *platformv1alpha1.NiFiCluster, phase platformv1alpha1.AutoscalingExecutionPhase, state platformv1alpha1.AutoscalingExecutionState, targetReplicas int32, blockedReason, failureReason, message string) {
	if phase == "" {
		clearAutoscalingExecution(cluster)
		return
	}
	execution := &cluster.Status.Autoscaling.Execution
	if execution.Phase != phase || execution.StartedAt == nil {
		now := metav1.NewTime(time.Now().UTC())
		execution.StartedAt = &now
	}
	if execution.Phase != phase || execution.State != state || execution.BlockedReason != blockedReason || execution.FailureReason != failureReason || execution.Message != message || execution.LastTransitionTime == nil {
		now := metav1.NewTime(time.Now().UTC())
		execution.LastTransitionTime = &now
	}
	execution.Phase = phase
	execution.State = state
	execution.TargetReplicas = ptrTo(targetReplicas)
	execution.Message = message
	execution.BlockedReason = blockedReason
	execution.FailureReason = failureReason
}

func clearAutoscalingExecution(cluster *platformv1alpha1.NiFiCluster) {
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{}
}

func autoscalingTimedBlockStillActive(decision string) bool {
	untilIndex := strings.LastIndex(decision, "until ")
	if untilIndex < 0 {
		return false
	}
	value := strings.TrimSpace(decision[untilIndex+len("until "):])
	if value == "" {
		return false
	}
	timestamp, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return time.Now().UTC().Before(timestamp)
}

func mergeAutoscalingLowPressureStatus(current, persisted platformv1alpha1.AutoscalingLowPressureStatus) platformv1alpha1.AutoscalingLowPressureStatus {
	if current.Since != nil || current.LastObservedAt != nil || current.ConsecutiveSamples > 0 || current.Message != "" {
		return current
	}
	return *persisted.DeepCopy()
}

func updatedAutoscalingLowPressureStatus(previous, desired platformv1alpha1.AutoscalingStatus, samples autoscalingSignalCollection) platformv1alpha1.AutoscalingLowPressureStatus {
	if desired.Reason == autoscalingReasonProgressing {
		if previous.LowPressure.Since == nil && previous.LowPressureSince != nil {
			return platformv1alpha1.AutoscalingLowPressureStatus{
				Since:                      previous.LowPressureSince.DeepCopy(),
				ConsecutiveSamples:         defaultAutoscalingLowPressureSamples,
				RequiredConsecutiveSamples: defaultAutoscalingLowPressureSamples,
			}
		}
		return previous.LowPressure
	}
	if !autoscalingLowPressureObserved(samples) {
		return platformv1alpha1.AutoscalingLowPressureStatus{}
	}

	observedAt := desired.LastEvaluationTime
	if observedAt == nil {
		now := metav1.NewTime(time.Now().UTC())
		observedAt = &now
	}

	requiredSamples := previous.LowPressure.RequiredConsecutiveSamples
	if requiredSamples <= 0 {
		requiredSamples = defaultAutoscalingLowPressureSamples
	}

	lowPressure := platformv1alpha1.AutoscalingLowPressureStatus{
		Since:                      observedAt.DeepCopy(),
		LastObservedAt:             observedAt.DeepCopy(),
		ConsecutiveSamples:         1,
		RequiredConsecutiveSamples: requiredSamples,
		FlowFilesQueued:            samples.QueuePressure.FlowFilesQueued,
		BytesQueued:                samples.QueuePressure.BytesQueued,
		BytesQueuedObserved:        samples.QueuePressure.BytesQueuedObserved,
	}
	if previous.LowPressure.Since != nil {
		lowPressure.Since = previous.LowPressure.Since.DeepCopy()
	}
	if previous.LowPressure.Since == nil && previous.LowPressureSince != nil {
		lowPressure.Since = previous.LowPressureSince.DeepCopy()
		lowPressure.ConsecutiveSamples = lowPressure.RequiredConsecutiveSamples
	}
	if previous.LowPressure.LastObservedAt != nil && observedAt.Time.After(previous.LowPressure.LastObservedAt.Time) {
		lowPressure.ConsecutiveSamples = previous.LowPressure.ConsecutiveSamples + 1
	}
	if lowPressure.ConsecutiveSamples > lowPressure.RequiredConsecutiveSamples {
		lowPressure.ConsecutiveSamples = lowPressure.RequiredConsecutiveSamples
	}
	lowPressure.Message = fmt.Sprintf(
		"zero backlog observed across %d/%d consecutive evaluations; queuedFlowFiles=%d queuedBytes=%s",
		lowPressure.ConsecutiveSamples,
		lowPressure.RequiredConsecutiveSamples,
		lowPressure.FlowFilesQueued,
		formatObservedBytes(lowPressure.BytesQueued, lowPressure.BytesQueuedObserved),
	)
	return lowPressure
}

func autoscalingLowPressureRequirementMet(status platformv1alpha1.AutoscalingLowPressureStatus) bool {
	required := status.RequiredConsecutiveSamples
	if required <= 0 {
		required = defaultAutoscalingLowPressureSamples
	}
	return status.Since != nil && status.ConsecutiveSamples >= required
}

func updatedAutoscalingLowPressureSince(previous, desired platformv1alpha1.AutoscalingStatus, samples autoscalingSignalCollection) *metav1.Time {
	status := updatedAutoscalingLowPressureStatus(previous, desired, samples)
	return status.Since
}

func autoscalingLowPressureObserved(samples autoscalingSignalCollection) bool {
	return samples.QueuePressure.LowPressure
}

func conditionIsTrue(condition *metav1.Condition) bool {
	return condition != nil && condition.Status == metav1.ConditionTrue
}
