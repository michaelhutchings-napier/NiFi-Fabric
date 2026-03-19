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
	autoscalingReasonDisabled                       = "Disabled"
	autoscalingReasonTargetNotResolved              = "TargetNotResolved"
	autoscalingReasonUnmanagedTarget                = "UnmanagedTarget"
	autoscalingReasonHibernated                     = "Hibernated"
	autoscalingReasonProgressing                    = "Progressing"
	autoscalingReasonDegraded                       = "Degraded"
	autoscalingReasonUnavailable                    = "Unavailable"
	autoscalingReasonBelowMinReplicas               = "BelowMinReplicas"
	autoscalingReasonAboveMaxReplicas               = "AboveMaxReplicas"
	autoscalingReasonExternalScaleUp                = "ExternalScaleUpRequested"
	autoscalingReasonExternalScaleDown              = "ExternalScaleDownRequested"
	autoscalingReasonScaleUpPending                 = "ScaleUpConfidencePending"
	autoscalingReasonQueuePressure                  = "QueuePressureDetected"
	autoscalingReasonCPUSaturation                  = "CPUSaturationDetected"
	autoscalingReasonLowPressure                    = "LowPressureDetected"
	autoscalingReasonMaxReplicasReached             = "MaxReplicasReached"
	autoscalingReasonNoActionableInput              = "NoActionableSignals"
	autoscalingExternalReasonBlocked                = "ExternalIntentBlocked"
	autoscalingExternalReasonBlockedDisabled        = "ExternalIntentBlockedDisabled"
	autoscalingExternalReasonScaleUpCooldownActive  = "ExternalScaleUpCooldownActive"
	autoscalingExternalReasonScaleDownWaitingLow    = "ExternalScaleDownWaitingForLowPressure"
	autoscalingExternalReasonScaleDownWaitingStable = "ExternalScaleDownWaitingForStability"
	autoscalingExternalReasonScaleDownCooldown      = "ExternalScaleDownCooldownActive"
	autoscalingExternalReasonSatisfied              = "ExternalRecommendationSatisfied"
	autoscalingExternalReasonScaleDownDisabled      = "ExternalScaleDownDisabled"
	autoscalingExternalReasonScaleDownMinSatisfied  = "ExternalScaleDownMinimumSatisfied"

	defaultAutoscalingScaleUpCooldown        = 5 * time.Minute
	defaultAutoscalingScaleDownCooldown      = 10 * time.Minute
	defaultAutoscalingScaleDownStabilization = 5 * time.Minute
	defaultAutoscalingScaleDownMaxSteps      = 1
	defaultAutoscalingLowPressureSamples     = 3
	autoscalingLowPressureExtraSamples       = 2
	autoscalingLowPressureThreadDivisor      = 4
	autoscalingScaleUpThreadDivisor          = 4
	autoscalingScaleUpThreadNumerator        = 3
	autoscalingQueueBytesPerThreadPressure   = 4 * 1024 * 1024
	autoscalingQueueBytesPerThreadSevere     = 16 * 1024 * 1024
	autoscalingCPUPressureRatio              = 0.75
	autoscalingCPUSevereRatio                = 1.25
)

type autoscalingExternalEvaluation struct {
	status                   platformv1alpha1.AutoscalingExternalStatus
	effectiveReplicas        *int32
	boundedRequestedReplicas *int32
}

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

	latest, err := r.liveCluster(ctx, client.ObjectKeyFromObject(cluster))
	if err != nil {
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
		return autoscalingDecisionWithContext(cluster, status, "NoScale: autoscaling is not in enforced mode")
	}
	if status.RecommendedReplicas == nil {
		return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScale: %s", autoscalingStatusMessageForCluster(cluster, status)))
	}

	currentReplicas := cluster.Status.Replicas.Desired
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	if status.External.Observed && status.External.ScaleDownIgnored && status.External.RequestedReplicas != nil {
		if status.External.Message != "" {
			return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", status.External.Message))
		}
		return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: external %s request for %d replicas is ignored", emptyIfUnset(string(status.External.Source), "external"), *status.External.RequestedReplicas))
	}
	switch {
	case recommendedReplicas > currentReplicas:
		if !policy.ScaleUp.Enabled {
			return autoscalingDecisionWithContext(cluster, status, "NoScaleUp: scale-up is not enabled")
		}
		if status.LastScaleUpTime != nil {
			nextEligibleTime := status.LastScaleUpTime.Time.Add(autoscalingScaleUpCooldown(policy))
			if time.Now().UTC().Before(nextEligibleTime) {
				return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleUp: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339)))
			}
		}
		return autoscalingDecisionWithContext(cluster, status, "NoScaleUp: waiting for the steady-state autoscaling executor")
	case recommendedReplicas < currentReplicas:
		if !policy.ScaleDown.Enabled {
			return autoscalingDecisionWithContext(cluster, status, "NoScaleDown: scale-down is not enabled")
		}
		if status.LowPressureSince == nil {
			return autoscalingDecisionWithContext(cluster, status, "NoScaleDown: low pressure is not currently observed")
		}
		if !autoscalingLowPressureRequirementMet(status.LowPressure) {
			return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf(
				"NoScaleDown: low pressure needs %d/%d consecutive zero-backlog evaluations before any scale-down step",
				status.LowPressure.ConsecutiveSamples,
				status.LowPressure.RequiredConsecutiveSamples,
			))
		}
		nextStableTime := status.LowPressureSince.Time.Add(autoscalingScaleDownStabilizationWindow(policy))
		if time.Now().UTC().Before(nextStableTime) {
			return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: low pressure must remain stable until %s", nextStableTime.UTC().Format(time.RFC3339)))
		}
		if status.LastScaleDownTime != nil {
			nextEligibleTime := status.LastScaleDownTime.Time.Add(autoscalingScaleDownCooldown(policy))
			if time.Now().UTC().Before(nextEligibleTime) {
				return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339)))
			}
		}
		return autoscalingDecisionWithContext(cluster, status, "NoScaleDown: waiting for the steady-state autoscaling executor")
	}
	if status.Reason == autoscalingReasonScaleUpPending {
		return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleUp: %s", autoscalingStatusMessageForCluster(cluster, status)))
	}
	if currentReplicas > autoscalingMinReplicas(policy, currentReplicas) && policy.ScaleDown.Enabled {
		if reason := autoscalingLowPressureBlockedReasonFromSignals(status.Signals); reason != "" {
			return autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", reason))
		}
	}
	return autoscalingDecisionWithContext(cluster, status, "NoScale: recommended replicas are already satisfied")
}

func (r *NiFiClusterReconciler) buildAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	target, _, err := r.liveTargetState(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name})
	if err != nil {
		policy := cluster.Spec.Autoscaling
		currentReplicas := cluster.Status.Replicas.Desired
		minReplicas := autoscalingMinReplicas(policy, currentReplicas)
		maxReplicas := autoscalingMaxReplicas(policy, minReplicas, currentReplicas)
		return platformv1alpha1.AutoscalingStatus{
			Reason:   autoscalingReasonTargetNotResolved,
			External: buildAutoscalingExternalEvaluation(policy, currentReplicas, minReplicas, maxReplicas).status,
		}, autoscalingSignalCollection{}
	}

	return r.buildAutoscalingStatusForTarget(ctx, cluster, target)
}

func (r *NiFiClusterReconciler) buildAutoscalingStatusForTarget(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	policy := cluster.Spec.Autoscaling
	currentReplicas := cluster.Status.Replicas.Desired
	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	maxReplicas := autoscalingMaxReplicas(policy, minReplicas, currentReplicas)
	external := buildAutoscalingExternalEvaluation(policy, currentReplicas, minReplicas, maxReplicas).status
	blocked, blockedReason := blockedAutoscalingStatus(cluster)
	if blocked {
		status := platformv1alpha1.AutoscalingStatus{Reason: blockedReason, External: external}
		return refineAutoscalingExternalHandling(cluster, status), autoscalingSignalCollection{}
	}
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		status := platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonDisabled, External: external}
		return refineAutoscalingExternalHandling(cluster, status), autoscalingSignalCollection{}
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

	previous := r.previousAutoscalingStatus(ctx, cluster)
	collection = qualifyAutoscalingSignalCollection(previous, collection)
	return refineAutoscalingExternalHandling(cluster, buildAutoscalingSteadyStateStatus(cluster, previous, policy, collection)), collection
}

func (r *NiFiClusterReconciler) maybeExecuteAutoscalingScaleUp(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (bool, ctrl.Result, error) {
	status, _ := r.buildAutoscalingStatusForTarget(ctx, cluster, target)
	executionState := r.autoscalingExecutionState(ctx, cluster, target)

	policy := cluster.Spec.Autoscaling
	mode := autoscalingMode(policy)
	switch mode {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScale: autoscaling is not in enforced mode")
		return false, ctrl.Result{}, nil
	}

	if !policy.ScaleUp.Enabled {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScaleUp: scale-up is not enabled")
		return false, ctrl.Result{}, nil
	}
	if status.RecommendedReplicas == nil {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleUp: %s", autoscalingStatusMessageForCluster(cluster, status)))
		return false, ctrl.Result{}, nil
	}

	currentReplicas := executionState.currentReplicas
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	switch {
	case recommendedReplicas < currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScaleUp: recommended replicas require a smaller cluster")
		return false, ctrl.Result{}, nil
	case recommendedReplicas == currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingNoScaleDecision(cluster, status)
		return false, ctrl.Result{}, nil
	}

	cooldown := autoscalingScaleUpCooldown(policy)
	if executionState.lastScaleUpTime != nil && cooldown > 0 {
		nextEligibleTime := executionState.lastScaleUpTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleUp: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339)))
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
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScaleUp: bounded recommendation does not allow a larger target size")
		return false, ctrl.Result{}, nil
	}

	scaledTarget := target.DeepCopy()
	scaledTarget.Spec.Replicas = ptrTo(desiredReplicas)
	if err := r.Update(ctx, scaledTarget); err != nil {
		return false, ctrl.Result{}, fmt.Errorf("update StatefulSet replicas for autoscaling scale-up: %w", err)
	}

	now := metav1.NewTime(time.Now().UTC())
	decision := fmt.Sprintf("ScaleUp: increased target StatefulSet replicas from %d to %d", currentReplicas, desiredReplicas)
	cluster.Status.Autoscaling.LastScaleUpTime = &now
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle, platformv1alpha1.AutoscalingExecutionStateRunning, desiredReplicas, "", "", fmt.Sprintf("Waiting for the autoscaling scale-up step to settle at %d replicas", desiredReplicas))
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, decision)
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleUp", fmt.Sprintf("%s because %s", decision, autoscalingStatusMessageForCluster(cluster, status)))
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

	latestCluster, err := r.liveCluster(ctx, client.ObjectKeyFromObject(cluster))
	if err != nil {
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

func buildAutoscalingSteadyStateStatus(cluster *platformv1alpha1.NiFiCluster, _ platformv1alpha1.AutoscalingStatus, policy platformv1alpha1.AutoscalingPolicy, collection autoscalingSignalCollection) platformv1alpha1.AutoscalingStatus {
	currentReplicas := cluster.Status.Replicas.Desired
	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	maxReplicas := autoscalingMaxReplicas(policy, minReplicas, currentReplicas)
	externalEvaluation := buildAutoscalingExternalEvaluation(policy, currentReplicas, minReplicas, maxReplicas)
	external := externalEvaluation.status

	recommended := currentReplicas
	reason := autoscalingReasonNoActionableInput

	switch {
	case currentReplicas < minReplicas:
		recommended = minReplicas
		reason = autoscalingReasonBelowMinReplicas
	case currentReplicas > maxReplicas:
		recommended = maxReplicas
		reason = autoscalingReasonAboveMaxReplicas
	case external.Actionable && externalEvaluation.effectiveReplicas != nil:
		recommended = *externalEvaluation.effectiveReplicas
		if recommended < currentReplicas {
			reason = autoscalingReasonExternalScaleDown
		} else {
			reason = autoscalingReasonExternalScaleUp
		}
	case autoscalingScaleUpConfidencePending(collection):
		recommended = currentReplicas
		reason = autoscalingReasonScaleUpPending
	case collection.QueuePressure.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonQueuePressure
	case collection.CPU.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonCPUSaturation
	case autoscalingLowPressureObserved(collection) && currentReplicas > minReplicas && !externalPreventsScaleDown(externalEvaluation, currentReplicas):
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
		External:            external,
	}
}

func (r *NiFiClusterReconciler) previousAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) platformv1alpha1.AutoscalingStatus {
	if r == nil || r.APIReader == nil {
		return cluster.Status.Autoscaling
	}
	latest, err := r.liveCluster(ctx, client.ObjectKeyFromObject(cluster))
	if err != nil {
		return cluster.Status.Autoscaling
	}
	return latest.Status.Autoscaling
}

func buildAutoscalingExternalEvaluation(policy platformv1alpha1.AutoscalingPolicy, currentReplicas, minReplicas, maxReplicas int32) autoscalingExternalEvaluation {
	if !policy.External.Enabled || policy.External.Source == "" {
		return autoscalingExternalEvaluation{}
	}

	requested := policy.External.RequestedReplicas
	status := platformv1alpha1.AutoscalingExternalStatus{
		Observed:          true,
		Source:            policy.External.Source,
		RequestedReplicas: ptrTo(requested),
	}

	switch {
	case requested > currentReplicas:
		bounded := requested
		if bounded < minReplicas {
			bounded = minReplicas
		}
		if bounded > maxReplicas {
			bounded = maxReplicas
		}
		if bounded > currentReplicas {
			status.BoundedReplicas = ptrTo(bounded)
			status.Actionable = true
			status.Reason = autoscalingReasonExternalScaleUp
			if bounded != requested {
				status.Message = fmt.Sprintf("external %s requested %d replicas; controller bounded the scale-up intent to %d within autoscaling policy", policy.External.Source, requested, bounded)
			} else {
				status.Message = fmt.Sprintf("external %s requested scale-up intent to %d replicas through NiFiCluster /scale", policy.External.Source, bounded)
			}
			return autoscalingExternalEvaluation{
				status:                   status,
				effectiveReplicas:        ptrTo(bounded),
				boundedRequestedReplicas: ptrTo(bounded),
			}
		}
		status.Reason = autoscalingReasonMaxReplicasReached
		status.BoundedReplicas = ptrTo(currentReplicas)
		status.Message = fmt.Sprintf("external %s requested %d replicas, but the configured autoscaling maximum keeps the recommendation at %d", policy.External.Source, requested, currentReplicas)
		return autoscalingExternalEvaluation{
			status:                   status,
			boundedRequestedReplicas: ptrTo(currentReplicas),
		}
	case requested < currentReplicas:
		if !policy.External.ScaleDownEnabled {
			status.BoundedReplicas = ptrTo(currentReplicas)
			status.ScaleDownIgnored = true
			status.Reason = autoscalingExternalReasonScaleDownDisabled
			status.Message = fmt.Sprintf("external %s requested scale-down intent to %d replicas, but external scale-down intent is disabled; controller-owned low-pressure scale-down remains the only supported path", policy.External.Source, requested)
			return autoscalingExternalEvaluation{
				status:                   status,
				boundedRequestedReplicas: ptrTo(requested),
			}
		}
		bounded := requested
		if bounded < minReplicas {
			bounded = minReplicas
		}
		if bounded < currentReplicas {
			status.BoundedReplicas = ptrTo(bounded)
			status.Actionable = true
			status.Reason = autoscalingReasonExternalScaleDown
			if bounded != requested {
				status.Message = fmt.Sprintf("external %s requested %d replicas; controller bounded the best-effort scale-down intent to %d within autoscaling policy and will only act after safe scale-down checks pass", policy.External.Source, requested, bounded)
			} else {
				status.Message = fmt.Sprintf("external %s requested best-effort scale-down intent to %d replicas through NiFiCluster /scale; the controller will only act after the existing safe scale-down checks pass", policy.External.Source, bounded)
			}
			return autoscalingExternalEvaluation{
				status:                   status,
				effectiveReplicas:        ptrTo(bounded),
				boundedRequestedReplicas: ptrTo(bounded),
			}
		}
		status.BoundedReplicas = ptrTo(minReplicas)
		status.ScaleDownIgnored = true
		status.Reason = autoscalingExternalReasonScaleDownMinSatisfied
		status.Message = fmt.Sprintf("external %s requested scale-down intent to %d replicas, but minReplicas %d already keeps the cluster at its lowest allowed size", policy.External.Source, requested, minReplicas)
		return autoscalingExternalEvaluation{
			status:                   status,
			boundedRequestedReplicas: ptrTo(minReplicas),
		}
	default:
		status.BoundedReplicas = ptrTo(currentReplicas)
		status.Reason = autoscalingExternalReasonSatisfied
		status.Message = fmt.Sprintf("external %s currently matches the running replica count at %d", policy.External.Source, currentReplicas)
		return autoscalingExternalEvaluation{
			status:                   status,
			boundedRequestedReplicas: ptrTo(currentReplicas),
		}
	}
}

func refineAutoscalingExternalHandling(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) platformv1alpha1.AutoscalingStatus {
	if !status.External.Observed || status.External.RequestedReplicas == nil || cluster == nil {
		return status
	}

	external := status.External
	currentReplicas := cluster.Status.Replicas.Desired
	boundedReplicas := currentReplicas
	if external.BoundedReplicas != nil {
		boundedReplicas = *external.BoundedReplicas
	}

	switch status.Reason {
	case autoscalingReasonDisabled:
		external.Actionable = false
		external.Reason = autoscalingExternalReasonBlockedDisabled
		external.Message = fmt.Sprintf("%s; autoscaling is currently disabled", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)))
	case autoscalingReasonTargetNotResolved, autoscalingReasonUnmanagedTarget, autoscalingReasonHibernated, autoscalingReasonProgressing, autoscalingReasonDegraded, autoscalingReasonUnavailable:
		external.Actionable = false
		external.Reason = autoscalingExternalReasonBlocked
		external.Message = fmt.Sprintf("%s; the controller is currently blocked because %s", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)), autoscalingExternalBlockedSummary(cluster, status.Reason))
	case autoscalingReasonExternalScaleUp:
		if boundedReplicas > currentReplicas {
			nextEligibleTime, coolingDown := autoscalingScaleUpNextEligibleTime(cluster.Spec.Autoscaling, latestAutoscalingTimestamp(status.LastScaleUpTime, cluster.Status.Autoscaling.LastScaleUpTime))
			if coolingDown {
				external.Actionable = false
				external.Reason = autoscalingExternalReasonScaleUpCooldownActive
				external.Message = fmt.Sprintf("%s; the controller will wait for scale-up cooldown until %s", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)), nextEligibleTime.UTC().Format(time.RFC3339))
			}
		}
	case autoscalingReasonExternalScaleDown:
		if boundedReplicas < currentReplicas {
			switch {
			case !cluster.Spec.Autoscaling.ScaleDown.Enabled:
				external.Actionable = false
				external.Reason = autoscalingExternalReasonScaleDownDisabled
				external.Message = fmt.Sprintf("%s; controller scale-down is not enabled", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)))
			case status.LowPressureSince == nil:
				external.Actionable = false
				external.Reason = autoscalingExternalReasonScaleDownWaitingLow
				external.Message = fmt.Sprintf("%s; the controller is still waiting for low pressure before any safe scale-down step", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)))
			case !autoscalingLowPressureRequirementMet(status.LowPressure):
				external.Actionable = false
				external.Reason = autoscalingExternalReasonScaleDownWaitingStable
				external.Message = fmt.Sprintf("%s; the controller still needs %d/%d consecutive low-pressure evaluations before scale-down can start", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)), status.LowPressure.ConsecutiveSamples, status.LowPressure.RequiredConsecutiveSamples)
			default:
				nextStableTime, stabilizing := autoscalingScaleDownNextStableTime(cluster.Spec.Autoscaling, status.LowPressureSince)
				if stabilizing {
					external.Actionable = false
					external.Reason = autoscalingExternalReasonScaleDownWaitingStable
					external.Message = fmt.Sprintf("%s; the controller is keeping the cluster stable until %s before any safe scale-down step", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)), nextStableTime.UTC().Format(time.RFC3339))
					break
				}
				nextEligibleTime, coolingDown := autoscalingScaleDownNextEligibleTime(cluster.Spec.Autoscaling, latestAutoscalingTimestamp(status.LastScaleDownTime, cluster.Status.Autoscaling.LastScaleDownTime))
				if coolingDown {
					external.Actionable = false
					external.Reason = autoscalingExternalReasonScaleDownCooldown
					external.Message = fmt.Sprintf("%s; the controller will wait for scale-down cooldown until %s", emptyIfUnset(external.Message, autoscalingExternalRequestSummary(external, currentReplicas)), nextEligibleTime.UTC().Format(time.RFC3339))
				}
			}
		}
	}

	status.External = external
	return status
}

func latestAutoscalingTimestamp(primary, fallback *metav1.Time) *metav1.Time {
	switch {
	case primary == nil:
		return fallback
	case fallback == nil:
		return primary
	case fallback.After(primary.Time):
		return fallback
	default:
		return primary
	}
}

func autoscalingScaleUpNextEligibleTime(policy platformv1alpha1.AutoscalingPolicy, lastScaleUpTime *metav1.Time) (time.Time, bool) {
	if lastScaleUpTime == nil {
		return time.Time{}, false
	}
	nextEligibleTime := lastScaleUpTime.Time.Add(autoscalingScaleUpCooldown(policy))
	return nextEligibleTime, time.Now().UTC().Before(nextEligibleTime)
}

func autoscalingScaleDownNextEligibleTime(policy platformv1alpha1.AutoscalingPolicy, lastScaleDownTime *metav1.Time) (time.Time, bool) {
	if lastScaleDownTime == nil {
		return time.Time{}, false
	}
	nextEligibleTime := lastScaleDownTime.Time.Add(autoscalingScaleDownCooldown(policy))
	return nextEligibleTime, time.Now().UTC().Before(nextEligibleTime)
}

func autoscalingScaleDownNextStableTime(policy platformv1alpha1.AutoscalingPolicy, lowPressureSince *metav1.Time) (time.Time, bool) {
	if lowPressureSince == nil {
		return time.Time{}, false
	}
	nextStableTime := lowPressureSince.Time.Add(autoscalingScaleDownStabilizationWindow(policy))
	return nextStableTime, time.Now().UTC().Before(nextStableTime)
}

func autoscalingExternalRequestSummary(external platformv1alpha1.AutoscalingExternalStatus, currentReplicas int32) string {
	requestedReplicas := derefOptionalInt32(external.RequestedReplicas)
	boundedReplicas := requestedReplicas
	if external.BoundedReplicas != nil {
		boundedReplicas = *external.BoundedReplicas
	}
	switch {
	case requestedReplicas > currentReplicas:
		if boundedReplicas != requestedReplicas {
			return fmt.Sprintf("external %s requested %d replicas and the controller bounded that scale-up intent to %d", external.Source, requestedReplicas, boundedReplicas)
		}
		return fmt.Sprintf("external %s requested scale-up intent to %d replicas through NiFiCluster /scale", external.Source, boundedReplicas)
	case requestedReplicas < currentReplicas:
		if boundedReplicas != requestedReplicas {
			return fmt.Sprintf("external %s requested %d replicas and the controller bounded that best-effort scale-down intent to %d", external.Source, requestedReplicas, boundedReplicas)
		}
		return fmt.Sprintf("external %s requested best-effort scale-down intent to %d replicas through NiFiCluster /scale", external.Source, boundedReplicas)
	default:
		return fmt.Sprintf("external %s currently matches the running replica count at %d", external.Source, currentReplicas)
	}
}

func autoscalingExternalBlockedSummary(cluster *platformv1alpha1.NiFiCluster, reason string) string {
	switch reason {
	case autoscalingReasonTargetNotResolved:
		if cluster.Spec.TargetRef.Name != "" {
			return fmt.Sprintf("target StatefulSet %q is not yet resolved", cluster.Spec.TargetRef.Name)
		}
		return "the target StatefulSet is not yet resolved"
	case autoscalingReasonUnmanagedTarget:
		return "the target StatefulSet is unmanaged"
	case autoscalingReasonHibernated:
		if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated {
			return "desiredState is Hibernated"
		}
		if condition := cluster.GetCondition(platformv1alpha1.ConditionHibernated); conditionIsTrue(condition) && condition.Message != "" {
			return fmt.Sprintf("the cluster is hibernated: %s", condition.Message)
		}
		return "the cluster is hibernated"
	case autoscalingReasonProgressing:
		return emptyIfUnset(autoscalingLifecycleProgressMessage(cluster), "controller-owned lifecycle work is in progress")
	case autoscalingReasonDegraded:
		if condition := cluster.GetCondition(platformv1alpha1.ConditionDegraded); conditionIsTrue(condition) && condition.Message != "" {
			return fmt.Sprintf("the cluster is degraded: %s", condition.Message)
		}
		return "the cluster is degraded"
	case autoscalingReasonUnavailable:
		if condition := cluster.GetCondition(platformv1alpha1.ConditionAvailable); condition != nil && condition.Message != "" {
			return fmt.Sprintf("the cluster is not yet available: %s", condition.Message)
		}
		return "the cluster is not yet available"
	default:
		return "controller-owned lifecycle safety checks are still blocking execution"
	}
}

func externalPreventsScaleDown(external autoscalingExternalEvaluation, currentReplicas int32) bool {
	if external.boundedRequestedReplicas == nil {
		return false
	}
	return *external.boundedRequestedReplicas >= currentReplicas
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

func autoscalingScaleDownMaxSequentialSteps(policy platformv1alpha1.AutoscalingPolicy) int32 {
	if policy.ScaleDown.MaxSequentialSteps > 0 {
		return policy.ScaleDown.MaxSequentialSteps
	}
	return defaultAutoscalingScaleDownMaxSteps
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
	if left.External.Observed != right.External.Observed ||
		left.External.Source != right.External.Source ||
		!equalOptionalInt32(left.External.RequestedReplicas, right.External.RequestedReplicas) ||
		!equalOptionalInt32(left.External.BoundedReplicas, right.External.BoundedReplicas) ||
		left.External.Actionable != right.External.Actionable ||
		left.External.ScaleDownIgnored != right.External.ScaleDownIgnored ||
		left.External.Reason != right.External.Reason ||
		left.External.Message != right.External.Message {
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
	if left.External.Observed != right.External.Observed ||
		left.External.Source != right.External.Source ||
		!equalOptionalInt32(left.External.RequestedReplicas, right.External.RequestedReplicas) ||
		!equalOptionalInt32(left.External.BoundedReplicas, right.External.BoundedReplicas) ||
		left.External.Actionable != right.External.Actionable ||
		left.External.ScaleDownIgnored != right.External.ScaleDownIgnored ||
		left.External.Reason != right.External.Reason {
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

func autoscalingStatusMessageForCluster(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	if cluster != nil {
		switch status.Reason {
		case autoscalingReasonTargetNotResolved:
			if cluster.Spec.TargetRef.Name != "" {
				return appendAutoscalingExternalMessage(fmt.Sprintf("Autoscaling is waiting for target StatefulSet %q to resolve", cluster.Spec.TargetRef.Name), status)
			}
		case autoscalingReasonHibernated:
			if condition := cluster.GetCondition(platformv1alpha1.ConditionHibernated); conditionIsTrue(condition) && condition.Message != "" {
				return appendAutoscalingExternalMessage(fmt.Sprintf("Autoscaling is blocked while hibernation or restore has precedence: %s", condition.Message), status)
			}
			if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated {
				return appendAutoscalingExternalMessage("Autoscaling is blocked because desiredState is Hibernated", status)
			}
		case autoscalingReasonProgressing:
			if message := autoscalingLifecycleProgressMessage(cluster); message != "" {
				return appendAutoscalingExternalMessage(fmt.Sprintf("Autoscaling is blocked while %s", message), status)
			}
		case autoscalingReasonDegraded:
			if condition := cluster.GetCondition(platformv1alpha1.ConditionDegraded); conditionIsTrue(condition) && condition.Message != "" {
				return appendAutoscalingExternalMessage(fmt.Sprintf("Autoscaling is blocked while the cluster is degraded: %s", condition.Message), status)
			}
		case autoscalingReasonUnavailable:
			if condition := cluster.GetCondition(platformv1alpha1.ConditionAvailable); condition != nil && condition.Message != "" {
				return appendAutoscalingExternalMessage(fmt.Sprintf("Autoscaling is blocked until the cluster is available: %s", condition.Message), status)
			}
		}
	}
	return autoscalingStatusMessage(cluster, status)
}

func autoscalingStatusMessage(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return appendAutoscalingExternalMessage("Autoscaling is disabled", status)
	case autoscalingReasonTargetNotResolved:
		return appendAutoscalingExternalMessage("Autoscaling is waiting for the target StatefulSet to resolve", status)
	case autoscalingReasonUnmanagedTarget:
		return appendAutoscalingExternalMessage("Autoscaling is blocked because the target StatefulSet is unmanaged", status)
	case autoscalingReasonHibernated:
		return appendAutoscalingExternalMessage("Autoscaling is blocked while the cluster is hibernated or restoring", status)
	case autoscalingReasonProgressing:
		return appendAutoscalingExternalMessage("Autoscaling is blocked while managed lifecycle work is in progress", status)
	case autoscalingReasonDegraded:
		return appendAutoscalingExternalMessage("Autoscaling is blocked while the cluster is degraded", status)
	case autoscalingReasonUnavailable:
		return appendAutoscalingExternalMessage("Autoscaling is blocked until the cluster is available", status)
	case autoscalingReasonBelowMinReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is below the configured minimum. %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingCapacityReasoning(cluster, status))
	case autoscalingReasonAboveMaxReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is above the configured maximum. %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingCapacityReasoning(cluster, status))
	case autoscalingReasonScaleUpPending:
		return fmt.Sprintf("Autoscaling holds at %d replicas because scale-up signal confidence is still forming and needs corroboration. %s Signals: %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingPendingCapacityReasoning(status), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonExternalScaleUp:
		return fmt.Sprintf("Autoscaling recommends %d replicas because %s", derefOptionalInt32(status.RecommendedReplicas), emptyIfUnset(status.External.Message, "external scale-up intent is active"))
	case autoscalingReasonExternalScaleDown:
		return fmt.Sprintf("Autoscaling recommends %d replicas because %s", derefOptionalInt32(status.RecommendedReplicas), emptyIfUnset(status.External.Message, "external scale-down intent is active"))
	case autoscalingReasonQueuePressure:
		return fmt.Sprintf("Autoscaling recommends %d replicas because root process-group backlog pressure is now sufficiently corroborated for scale-up. %s Signals: %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingCapacityReasoning(cluster, status), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonCPUSaturation:
		return fmt.Sprintf("Autoscaling recommends %d replicas because CPU saturation is now sufficiently corroborated for scale-up. %s Signals: %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingCapacityReasoning(cluster, status), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonLowPressure:
		return fmt.Sprintf("Autoscaling recommends %d replicas because NiFi root process-group backlog is repeatedly zero and no higher-priority scale-up pressure is active. %s Low-pressure evidence: %s. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), autoscalingCapacityReasoning(cluster, status), emptyIfUnset(status.LowPressure.Message, "none"), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonMaxReplicasReached:
		if status.External.Message != "" {
			return fmt.Sprintf("Autoscaling holds at %d replicas because %s", derefOptionalInt32(status.RecommendedReplicas), status.External.Message)
		}
		return fmt.Sprintf("Autoscaling is observing scale-up pressure, but the recommendation remains at %d replicas because the configured maximum has already been reached. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	default:
		summary := summarizeAutoscalingSignals(status.Signals)
		if status.External.Message != "" {
			if summary == "" {
				return fmt.Sprintf("Autoscaling recommends %d replicas; %s", derefOptionalInt32(status.RecommendedReplicas), status.External.Message)
			}
			return fmt.Sprintf("Autoscaling recommends %d replicas; %s. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), status.External.Message, summary)
		}
		if summary == "" {
			return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently available", derefOptionalInt32(status.RecommendedReplicas))
		}
		return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently driving a scale-up. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summary)
	}
}

func autoscalingCapacityReasoning(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	if cluster == nil || status.RecommendedReplicas == nil {
		return ""
	}

	currentReplicas := cluster.Status.Replicas.Desired
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	delta := recommendedReplicas - currentReplicas

	switch status.Reason {
	case autoscalingReasonBelowMinReplicas:
		if delta > 0 {
			return fmt.Sprintf("This restores the configured baseline capacity by adding %d replica(s).", delta)
		}
	case autoscalingReasonAboveMaxReplicas:
		if delta < 0 {
			return fmt.Sprintf("This returns the cluster to the configured maximum by removing %d replica(s).", -delta)
		}
	case autoscalingReasonQueuePressure:
		if delta > 0 {
			switch {
			case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "capacity is clearly insufficient"):
				return "One additional node is expected to add executor headroom immediately because queued bytes per timer-driven thread and executor saturation both show the current capacity is clearly insufficient."
			case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "simultaneous CPU saturation corroborates"):
				return "One additional node is expected to add executor headroom and help drain the current backlog because queue pressure and CPU saturation now agree that the cluster is capacity-constrained."
			default:
				return "One additional node is expected to add executor headroom and help drain the current backlog within the bounded one-step model."
			}
		}
	case autoscalingReasonCPUSaturation:
		if delta > 0 {
			if autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalCPU, "materially above processor capacity") {
				return "One additional node is expected to add CPU headroom because sustained load remains materially above the current processor budget."
			}
			return "One additional node is expected to add CPU headroom and reduce sustained saturation within the bounded one-step model."
		}
	case autoscalingReasonLowPressure:
		if delta < 0 {
			if autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "backlog is low") {
				return "One fewer node is expected to remain within the current low-pressure envelope because backlog is repeatedly quiet and timer-driven work is staying below the low-pressure threshold."
			}
			return "One fewer node is expected to remain within the current low-pressure envelope based on the observed quiet backlog and executor activity."
		}
	}

	return ""
}

func autoscalingPendingCapacityReasoning(status platformv1alpha1.AutoscalingStatus) string {
	switch {
	case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "queued bytes per timer-driven thread and executor usage are both elevated"):
		return "The strongest current evidence is elevated queued bytes per timer-driven thread plus growing executor usage, but the controller still wants one more corroborating evaluation before acting."
	case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "actionable threshold"):
		return "The strongest current evidence is that timer-driven executor saturation hit the actionable threshold, but the controller still wants one more corroborating evaluation before acting."
	case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalCPU, "actionable threshold"):
		return "The strongest current evidence is CPU pressure above the actionable threshold, but the controller still wants one more corroborating evaluation or backlog confirmation before acting."
	case autoscalingSignalsContain(status.Signals, platformv1alpha1.AutoscalingSignalCPU, "CPU pressure is building"):
		return "The strongest current evidence is rising CPU pressure, but it remains below the controller's actionable threshold."
	default:
		return ""
	}
}

func autoscalingSignalsContain(signals []platformv1alpha1.AutoscalingSignalStatus, signalType platformv1alpha1.AutoscalingSignal, fragment string) bool {
	for _, signal := range signals {
		if signal.Type != signalType {
			continue
		}
		if strings.Contains(strings.ToLower(signal.Message), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func appendAutoscalingExternalMessage(base string, status platformv1alpha1.AutoscalingStatus) string {
	if status.External.Message == "" {
		return base
	}
	return fmt.Sprintf("%s. External intent: %s", base, status.External.Message)
}

func autoscalingLifecycleProgressMessage(cluster *platformv1alpha1.NiFiCluster) string {
	if cluster == nil {
		return ""
	}
	if cluster.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseRunning && cluster.Status.LastOperation.Type != "" {
		return fmt.Sprintf("%s is running: %s", cluster.Status.LastOperation.Type, emptyIfUnset(cluster.Status.LastOperation.Message, "controller-owned lifecycle work is active"))
	}
	if cluster.Status.Autoscaling.Execution.Phase != "" && cluster.Status.Autoscaling.Execution.Message != "" {
		return fmt.Sprintf("autoscaling execution %s/%s is active: %s", cluster.Status.Autoscaling.Execution.Phase, cluster.Status.Autoscaling.Execution.State, cluster.Status.Autoscaling.Execution.Message)
	}
	if condition := cluster.GetCondition(platformv1alpha1.ConditionProgressing); conditionIsTrue(condition) && condition.Message != "" {
		return condition.Message
	}
	return ""
}

func autoscalingDecisionWithContext(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus, decision string) string {
	context := autoscalingDecisionContext(cluster, status)
	if decision == "" || context == "" {
		return decision
	}
	return fmt.Sprintf("%s [%s]", decision, context)
}

func autoscalingDecisionContext(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	if cluster == nil {
		return ""
	}
	parts := []string{
		fmt.Sprintf("mode=%s", autoscalingMode(cluster.Spec.Autoscaling)),
		fmt.Sprintf("current=%d", cluster.Status.Replicas.Desired),
	}
	if status.RecommendedReplicas != nil {
		parts = append(parts, fmt.Sprintf("recommended=%d", *status.RecommendedReplicas))
	}
	if status.External.RequestedReplicas != nil {
		parts = append(parts, fmt.Sprintf("requested=%d", *status.External.RequestedReplicas))
	}
	if status.External.BoundedReplicas != nil && (status.External.RequestedReplicas == nil || *status.External.BoundedReplicas != *status.External.RequestedReplicas) {
		parts = append(parts, fmt.Sprintf("externalBounded=%d", *status.External.BoundedReplicas))
	}
	execution := cluster.Status.Autoscaling.Execution
	if execution.Phase != "" && execution.State != "" {
		parts = append(parts, fmt.Sprintf("execution=%s/%s", execution.Phase, execution.State))
		if execution.TargetReplicas != nil {
			parts = append(parts, fmt.Sprintf("target=%d", *execution.TargetReplicas))
		}
		plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(execution)
		if plannedSteps > 0 {
			parts = append(parts, fmt.Sprintf("episode=%d/%d", completedSteps, plannedSteps))
		}
	} else {
		parts = append(parts, "execution=Idle")
	}
	if cluster.Status.NodeOperation.Purpose == platformv1alpha1.NodeOperationPurposeScaleDown && cluster.Status.NodeOperation.PodName != "" {
		parts = append(parts, fmt.Sprintf("pod=%s", cluster.Status.NodeOperation.PodName))
		if cluster.Status.NodeOperation.Stage != "" {
			parts = append(parts, fmt.Sprintf("stage=%s", cluster.Status.NodeOperation.Stage))
		}
	}
	return strings.Join(parts, ", ")
}

func autoscalingStatusOutcome(status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return "disabled"
	case autoscalingReasonBelowMinReplicas:
		return "increase"
	case autoscalingReasonScaleUpPending:
		return "hold"
	case autoscalingReasonAboveMaxReplicas, autoscalingReasonLowPressure, autoscalingReasonExternalScaleDown:
		return "decrease"
	case autoscalingReasonExternalScaleUp, autoscalingReasonQueuePressure, autoscalingReasonCPUSaturation:
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
		if merged.PlannedSteps == 0 {
			merged.PlannedSteps = persisted.PlannedSteps
		}
		if merged.CompletedSteps == 0 {
			merged.CompletedSteps = persisted.CompletedSteps
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
	candidates := strings.Fields(strings.TrimSpace(decision[untilIndex+len("until "):]))
	for _, candidate := range candidates {
		candidate = strings.Trim(candidate, "],.;)")
		timestamp, err := time.Parse(time.RFC3339, candidate)
		if err != nil {
			continue
		}
		return time.Now().UTC().Before(timestamp)
	}
	return false
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

	requiredSamples := autoscalingLowPressureRequiredSamples(samples)
	if previous.LowPressure.RequiredConsecutiveSamples > requiredSamples {
		requiredSamples = previous.LowPressure.RequiredConsecutiveSamples
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
		"zero backlog with low executor activity observed across %d/%d consecutive evaluations; queuedFlowFiles=%d queuedBytes=%s; %s",
		lowPressure.ConsecutiveSamples,
		lowPressure.RequiredConsecutiveSamples,
		lowPressure.FlowFilesQueued,
		formatObservedBytes(lowPressure.BytesQueued, lowPressure.BytesQueuedObserved),
		autoscalingLowPressureEvidenceDetails(samples.QueuePressure),
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
	if !samples.QueuePressure.LowPressure {
		return false
	}
	if !samples.QueuePressure.ThreadCountsObserved || samples.QueuePressure.MaxTimerDrivenThreads <= 0 {
		return true
	}
	return samples.QueuePressure.ActiveTimerDrivenThreads <= autoscalingLowPressureActiveThreadThreshold(samples.QueuePressure.MaxTimerDrivenThreads)
}

func autoscalingLowPressureRequiredSamples(samples autoscalingSignalCollection) int32 {
	required := int32(defaultAutoscalingLowPressureSamples)
	if !samples.QueuePressure.BytesQueuedObserved {
		required += autoscalingLowPressureExtraSamples
	}
	if !samples.QueuePressure.ThreadCountsObserved || samples.QueuePressure.MaxTimerDrivenThreads <= 0 {
		required += autoscalingLowPressureExtraSamples
	}
	return required
}

func autoscalingLowPressureActiveThreadThreshold(maxThreads int32) int32 {
	if maxThreads <= 0 {
		return 1
	}
	threshold := maxThreads / autoscalingLowPressureThreadDivisor
	if threshold < 1 {
		return 1
	}
	return threshold
}

func autoscalingScaleUpThreadThreshold(maxThreads int32) int32 {
	if maxThreads <= 0 {
		return 1
	}
	threshold := (maxThreads * autoscalingScaleUpThreadNumerator) / autoscalingScaleUpThreadDivisor
	if (maxThreads*autoscalingScaleUpThreadNumerator)%autoscalingScaleUpThreadDivisor != 0 {
		threshold++
	}
	if threshold < 1 {
		return 1
	}
	if threshold > maxThreads {
		return maxThreads
	}
	return threshold
}

func autoscalingLowPressureEvidenceDetails(sample autoscalingQueuePressureSample) string {
	details := make([]string, 0, 3)
	if sample.BytesQueuedObserved {
		details = append(details, "queued bytes observed")
	} else {
		details = append(details, "queued bytes unavailable so extra consecutive evidence is required")
	}
	if sample.ThreadCountsObserved && sample.MaxTimerDrivenThreads > 0 {
		details = append(details, fmt.Sprintf(
			"activeTimerDrivenThreads=%d/%d (allowed <=%d)",
			sample.ActiveTimerDrivenThreads,
			sample.MaxTimerDrivenThreads,
			autoscalingLowPressureActiveThreadThreshold(sample.MaxTimerDrivenThreads),
		))
	} else {
		details = append(details, "timer-driven thread counts unavailable so extra consecutive evidence is required")
	}
	return strings.Join(details, "; ")
}

func autoscalingLowPressureBlockedReason(samples autoscalingSignalCollection) string {
	sample := samples.QueuePressure
	if !sample.LowPressure {
		return ""
	}
	if sample.ThreadCountsObserved && sample.MaxTimerDrivenThreads > 0 {
		threshold := autoscalingLowPressureActiveThreadThreshold(sample.MaxTimerDrivenThreads)
		if sample.ActiveTimerDrivenThreads > threshold {
			return fmt.Sprintf(
				"zero backlog is not yet trustworthy because activeTimerDrivenThreads=%d/%d is above the low-pressure threshold %d",
				sample.ActiveTimerDrivenThreads,
				sample.MaxTimerDrivenThreads,
				threshold,
			)
		}
	}
	return ""
}

func autoscalingLowPressureBlockedReasonFromSignals(signals []platformv1alpha1.AutoscalingSignalStatus) string {
	const blockedPhrase = "backlog is zero but active timer-driven work is still above the low-pressure threshold"
	for _, signal := range signals {
		if signal.Type != platformv1alpha1.AutoscalingSignalQueuePressure || !signal.Available {
			continue
		}
		if strings.Contains(signal.Message, blockedPhrase) {
			return fmt.Sprintf("zero backlog is not yet trustworthy because %s", signal.Message)
		}
	}
	return ""
}

func conditionIsTrue(condition *metav1.Condition) bool {
	return condition != nil && condition.Status == metav1.ConditionTrue
}
