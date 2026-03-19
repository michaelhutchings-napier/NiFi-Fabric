package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const (
	autoscalingBlockedReasonScaleDownDrainPending         = "ScaleDownDrainPending"
	autoscalingBlockedReasonScaleDownDrainStalled         = "ScaleDownDrainStalled"
	autoscalingBlockedReasonScaleDownReadyPodsPending     = "ScaleDownReadyPodsPending"
	autoscalingBlockedReasonScaleDownReadyPodsStalled     = "ScaleDownReadyPodsStalled"
	autoscalingBlockedReasonScaleDownHealthGateBlocked    = "ScaleDownHealthGateBlocked"
	autoscalingBlockedReasonScaleDownHealthGateTimedOut   = "ScaleDownHealthGateTimedOut"
	autoscalingBlockedReasonScaleDownCooldownPending      = "ScaleDownCooldownPending"
	autoscalingBlockedReasonScaleDownStabilizationPending = "ScaleDownStabilizationPending"
	autoscalingBlockedReasonScaleDownPausedForRollout     = "ScaleDownPausedForRollout"
	autoscalingBlockedReasonScaleDownPausedForTLS         = "ScaleDownPausedForTLSObservation"
	autoscalingBlockedReasonScaleDownPausedForRestore     = "ScaleDownPausedForRestore"
	autoscalingBlockedReasonScaleDownPausedForHibernation = "ScaleDownPausedForHibernation"
	autoscalingBlockedReasonScaleDownCandidateMissing     = "ScaleDownCandidateMissing"
	autoscalingBlockedReasonScaleDownCandidateTerminating = "ScaleDownCandidateTerminating"
	autoscalingBlockedReasonScaleDownCandidateNotReady    = "ScaleDownCandidateNotReady"
)

type autoscalingScaleDownCandidateSelection struct {
	Pod             corev1.Pod
	Selected        bool
	ExpectedPodName string
	BlockedReason   string
	Message         string
	Detail          string
}

func autoscalingScaleDownEpisodeProgress(execution platformv1alpha1.AutoscalingExecutionStatus) (int32, int32) {
	plannedSteps := execution.PlannedSteps
	completedSteps := execution.CompletedSteps

	if plannedSteps == 0 && execution.Phase != "" {
		plannedSteps = 1
	}
	if completedSteps == 0 && execution.Phase == platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle {
		completedSteps = 1
	}
	if plannedSteps < completedSteps {
		plannedSteps = completedSteps
	}

	return plannedSteps, completedSteps
}

func setAutoscalingScaleDownEpisodeProgress(cluster *platformv1alpha1.NiFiCluster, plannedSteps, completedSteps int32) {
	execution := &cluster.Status.Autoscaling.Execution
	execution.PlannedSteps = plannedSteps
	execution.CompletedSteps = completedSteps
}

func autoscalingScaleDownEpisodeDesiredSteps(policy platformv1alpha1.AutoscalingPolicy, currentReplicas, recommendedReplicas, minReplicas int32) int32 {
	remainingRecommendation := currentReplicas - recommendedReplicas
	if remainingRecommendation <= 0 {
		return 0
	}

	remainingSafeReplicas := currentReplicas - minReplicas
	if remainingSafeReplicas <= 0 {
		return 0
	}

	plannedSteps := remainingRecommendation
	if plannedSteps > remainingSafeReplicas {
		plannedSteps = remainingSafeReplicas
	}
	maxSequentialSteps := autoscalingScaleDownMaxSequentialSteps(policy)
	if plannedSteps > maxSequentialSteps {
		plannedSteps = maxSequentialSteps
	}
	if plannedSteps < 1 {
		return 1
	}
	return plannedSteps
}

func autoscalingScaleDownEpisodeStepMessage(plannedSteps, completedSteps int32) string {
	if plannedSteps <= 1 {
		return "bounded one-step scale-down"
	}
	currentStep := completedSteps + 1
	if currentStep < 1 {
		currentStep = 1
	}
	if currentStep > plannedSteps {
		currentStep = plannedSteps
	}
	return fmt.Sprintf("step %d of %d in the bounded sequential scale-down episode", currentStep, plannedSteps)
}

func autoscalingScaleDownInProgress(cluster *platformv1alpha1.NiFiCluster) bool {
	if cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateFailed {
		return false
	}
	switch cluster.Status.Autoscaling.Execution.Phase {
	case platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle:
		return true
	}

	return autoscalingScaleDownLifecycleInProgress(cluster)
}

func autoscalingScaleDownLifecycleInProgress(cluster *platformv1alpha1.NiFiCluster) bool {
	if cluster.Status.NodeOperation.Purpose == platformv1alpha1.NodeOperationPurposeScaleDown && cluster.Status.NodeOperation.PodName != "" {
		return true
	}

	condition := cluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if conditionIsTrue(condition) {
		switch condition.Reason {
		case "AutoscalingScaleDown", "PreparingNodeForScaleDown", "WaitingForAutoscalingScaleDown":
			return true
		}
	}

	return cluster.Status.LastOperation.Type == "AutoscalingScaleDown" &&
		cluster.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseRunning
}

func (r *NiFiClusterReconciler) maybeExecuteAutoscalingScaleDown(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (bool, ctrl.Result, error) {
	status, samples := r.buildAutoscalingStatusForTarget(ctx, cluster, target)
	status.LowPressure = updatedAutoscalingLowPressureStatus(cluster.Status.Autoscaling, status, samples)
	status.LowPressureSince = status.LowPressure.Since
	executionState := r.autoscalingExecutionState(ctx, cluster, target)

	policy := cluster.Spec.Autoscaling
	mode := autoscalingMode(policy)
	switch mode {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScale: autoscaling is not in enforced mode")
		return false, ctrl.Result{}, nil
	}

	if !policy.ScaleDown.Enabled {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScaleDown: scale-down is not enabled")
		return false, ctrl.Result{}, nil
	}
	if status.RecommendedReplicas == nil {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", autoscalingStatusMessageForCluster(cluster, status)))
		return false, ctrl.Result{}, nil
	}

	currentReplicas := executionState.currentReplicas
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	switch {
	case recommendedReplicas > currentReplicas:
		return false, ctrl.Result{}, nil
	case recommendedReplicas == currentReplicas:
		return false, ctrl.Result{}, nil
	}

	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	if currentReplicas <= minReplicas {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: minimum replicas %d are already satisfied", minReplicas))
		return false, ctrl.Result{}, nil
	}
	plannedSteps := autoscalingScaleDownEpisodeDesiredSteps(policy, currentReplicas, recommendedReplicas, minReplicas)
	if plannedSteps <= 0 {
		return false, ctrl.Result{}, nil
	}
	if status.LowPressureSince == nil {
		if reason := autoscalingLowPressureBlockedReason(samples); reason != "" {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", reason))
		} else {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, "NoScaleDown: low pressure is not currently observed")
		}
		return false, ctrl.Result{}, nil
	}
	if !autoscalingLowPressureRequirementMet(status.LowPressure) {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf(
			"NoScaleDown: low pressure needs %d/%d consecutive zero-backlog evaluations before any scale-down step",
			status.LowPressure.ConsecutiveSamples,
			status.LowPressure.RequiredConsecutiveSamples,
		))
		return false, ctrl.Result{}, nil
	}

	stabilizationWindow := autoscalingScaleDownStabilizationWindow(policy)
	if stabilizationWindow > 0 {
		nextEligibleTime := status.LowPressureSince.Time.Add(stabilizationWindow)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: low pressure must remain stable until %s", nextEligibleTime.UTC().Format(time.RFC3339)))
			return false, ctrl.Result{}, nil
		}
	}

	cooldown := autoscalingScaleDownCooldown(policy)
	if executionState.lastScaleDownTime != nil && cooldown > 0 {
		nextEligibleTime := executionState.lastScaleDownTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339)))
			return false, ctrl.Result{}, nil
		}
	}
	setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, 0)

	candidate := selectAutoscalingScaleDownCandidate(target, pods, currentReplicas)
	if !candidate.Selected {
		log.FromContext(ctx).Info("Autoscaling scale-down candidate blocked", "candidate", candidate.ExpectedPodName, "reason", candidate.BlockedReason, "detail", candidate.Detail)
		r.blockAutoscalingScaleDownForCandidate(cluster, currentReplicas-1, candidate.BlockedReason, candidate.Message)
		return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}
	log.FromContext(ctx).Info("Autoscaling scale-down candidate selected", "candidate", candidate.Pod.Name, "detail", candidate.Detail)

	prepared, result, err := r.preparePodForScaleDown(ctx, cluster, target, pods, candidate.Pod)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if !prepared {
		if cluster.Status.Autoscaling.Execution.State == "" || cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateRunning {
			setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas-1, "", "", fmt.Sprintf("Preparing selected autoscaling scale-down candidate pod %s for safe removal as %s", candidate.Pod.Name, autoscalingScaleDownEpisodeStepMessage(plannedSteps, 0)))
		}
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingScaleDownPreparationDecision(cluster, candidate.Pod.Name, candidate.Detail)
		return true, result, nil
	}

	return r.executeAutoscalingScaleDownStep(ctx, cluster, target, candidate.Pod, currentReplicas, candidate.Detail)
}

func (r *NiFiClusterReconciler) reconcileAutoscalingScaleDown(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (ctrl.Result, error) {
	liveTarget, livePods, err := r.liveHibernationTargetState(ctx, target)
	if err != nil {
		return ctrl.Result{}, err
	}
	target = liveTarget
	pods = livePods
	clearNodeOperationIfPodMissing(cluster, pods)

	currentReplicas := derefInt32(target.Spec.Replicas)
	if currentNodeOpPod, ok := findNodeOperationPod(pods, cluster.Status.NodeOperation, platformv1alpha1.NodeOperationPurposeScaleDown); ok {
		setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas-1, "", "", fmt.Sprintf("Resuming safe autoscaling preparation for pod %s", currentNodeOpPod.Name))
		prepared, result, err := r.preparePodForScaleDown(ctx, cluster, target, pods, currentNodeOpPod)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !prepared {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingScaleDownPreparationDecision(cluster, currentNodeOpPod.Name, fmt.Sprintf("pod %s remains the selected autoscaling scale-down candidate while the controller resumes the blocked preparation step", currentNodeOpPod.Name))
			return result, nil
		}
		handled, result, err := r.executeAutoscalingScaleDownStep(ctx, cluster, target, currentNodeOpPod, currentReplicas, fmt.Sprintf("pod %s remained the selected autoscaling scale-down candidate across restart-safe resume", currentNodeOpPod.Name))
		if err != nil {
			return ctrl.Result{}, err
		}
		if handled {
			return result, nil
		}
	}

	if cluster.Status.Autoscaling.Execution.Phase == platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare {
		if cluster.Status.NodeOperation.PodName == "" &&
			cluster.Status.Autoscaling.Execution.CompletedSteps > 0 &&
			cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked {
			return r.resumeAutoscalingScaleDownEpisode(ctx, cluster, target, currentReplicas)
		}
		candidate := selectAutoscalingScaleDownCandidate(target, pods, currentReplicas)
		if !candidate.Selected {
			r.blockAutoscalingScaleDownForCandidate(cluster, currentReplicas-1, candidate.BlockedReason, candidate.Message)
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		log.FromContext(ctx).Info("Autoscaling scale-down candidate re-established after restart or pod churn", "candidate", candidate.Pod.Name, "detail", candidate.Detail)
		setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas-1, "", "", fmt.Sprintf("Re-establishing safe autoscaling preparation for selected candidate pod %s after pod churn or controller restart", candidate.Pod.Name))
		prepared, result, err := r.preparePodForScaleDown(ctx, cluster, target, pods, candidate.Pod)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !prepared {
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingScaleDownPreparationDecision(cluster, candidate.Pod.Name, candidate.Detail)
			return result, nil
		}
		handled, result, err := r.executeAutoscalingScaleDownStep(ctx, cluster, target, candidate.Pod, currentReplicas, candidate.Detail)
		if err != nil {
			return ctrl.Result{}, err
		}
		if handled {
			return result, nil
		}
	}

	if cluster.Status.Autoscaling.Execution.Phase == platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle &&
		cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked {
		setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas, "", "", fmt.Sprintf("Resuming autoscaling scale-down settlement at %d replicas after a blocked or restarted step", currentReplicas))
	}
	settled, result, err := r.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !settled {
		return result, nil
	}

	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(cluster.Status.Autoscaling.Execution)
	if plannedSteps > completedSteps {
		return r.resumeAutoscalingScaleDownEpisode(ctx, cluster, target, currentReplicas)
	}
	clearAutoscalingExecution(cluster)
	cluster.Status.LastOperation = succeededOperation("AutoscalingScaleDown", fmt.Sprintf("Managed autoscaling safely settled at %d replicas after completing %d/%d planned scale-down steps", currentReplicas, completedSteps, plannedSteps))
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "AutoscalingScaleDownSettled",
		Message:            fmt.Sprintf("Autoscaling scale-down settled successfully at %d replicas", currentReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDownSettled",
		Message:            "No autoscaling scale-down work is currently in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDownSettled",
		Message:            "No autoscaling scale-down failure is active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	settledStatus, samples := r.buildAutoscalingStatusForTarget(ctx, cluster, target)
	settledStatus.LowPressure = updatedAutoscalingLowPressureStatus(cluster.Status.Autoscaling, settledStatus, samples)
	settledStatus.LowPressureSince = settledStatus.LowPressure.Since
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingNoScaleDecision(cluster, settledStatus)
	if r.Recorder != nil {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownCompleted", fmt.Sprintf("Managed autoscaling settled safely at %d replicas after completing %d/%d planned sequential removals", currentReplicas, completedSteps, plannedSteps))
	}
	return steadyStateReconcileResult(cluster), nil
}

func (r *NiFiClusterReconciler) resumeAutoscalingScaleDownEpisode(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, currentReplicas int32) (ctrl.Result, error) {
	requalificationCluster := cluster.DeepCopy()
	requalificationCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Cluster is healthy",
		LastTransitionTime: metav1.Now(),
	})
	requalificationCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "NoDrift",
		Message:            "No work is in progress",
		LastTransitionTime: metav1.Now(),
	})
	requalificationCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "No degradation detected",
		LastTransitionTime: metav1.Now(),
	})
	requalificationCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	status, samples := r.buildAutoscalingStatusForTarget(ctx, requalificationCluster, target)
	status.LowPressure = updatedAutoscalingLowPressureStatus(cluster.Status.Autoscaling, status, samples)
	status.LowPressureSince = status.LowPressure.Since

	policy := cluster.Spec.Autoscaling
	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(cluster.Status.Autoscaling.Execution)
	if plannedSteps <= 0 {
		plannedSteps = 1
	}
	setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)

	stopEpisode := func(reason string) (ctrl.Result, error) {
		clearAutoscalingExecution(cluster)
		cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
		cluster.Status.LastOperation = succeededOperation("AutoscalingScaleDown", fmt.Sprintf("Managed autoscaling stopped the sequential scale-down episode at %d replicas after completing %d/%d planned steps", currentReplicas, completedSteps, plannedSteps))
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             "AutoscalingScaleDownSettled",
			Message:            fmt.Sprintf("Autoscaling scale-down settled successfully at %d replicas", currentReplicas),
			LastTransitionTime: metav1.Now(),
		})
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionProgressing,
			Status:             metav1.ConditionFalse,
			Reason:             "AutoscalingScaleDownSettled",
			Message:            "No autoscaling scale-down work is currently in progress",
			LastTransitionTime: metav1.Now(),
		})
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             "AutoscalingScaleDownSettled",
			Message:            "No autoscaling scale-down failure is active",
			LastTransitionTime: metav1.Now(),
		})
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionHibernated,
			Status:             metav1.ConditionFalse,
			Reason:             "Running",
			Message:            "Hibernation is not active",
			LastTransitionTime: metav1.Now(),
		})
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: sequential scale-down episode stopped after %d/%d completed steps because %s", completedSteps, plannedSteps, reason))
		if r.Recorder != nil {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownStopped", fmt.Sprintf("Managed autoscaling stopped the sequential scale-down episode at %d replicas after %d/%d completed steps because %s", currentReplicas, completedSteps, plannedSteps, reason))
		}
		return steadyStateReconcileResult(cluster), nil
	}

	if completedSteps >= plannedSteps {
		return stopEpisode("the planned sequential step budget is already exhausted")
	}
	if status.RecommendedReplicas == nil {
		return stopEpisode(autoscalingStatusMessageForCluster(cluster, status))
	}
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	if recommendedReplicas >= currentReplicas {
		return stopEpisode("fresh requalification no longer recommends a smaller cluster")
	}
	if currentReplicas <= minReplicas {
		return stopEpisode(fmt.Sprintf("minimum replicas %d are already satisfied", minReplicas))
	}
	if status.LowPressureSince == nil {
		if reason := autoscalingLowPressureBlockedReason(samples); reason != "" {
			return stopEpisode(reason)
		}
		return stopEpisode("low pressure is not currently observed")
	}
	if !autoscalingLowPressureRequirementMet(status.LowPressure) {
		return stopEpisode(fmt.Sprintf(
			"fresh requalification only has %d/%d consecutive zero-backlog evaluations",
			status.LowPressure.ConsecutiveSamples,
			status.LowPressure.RequiredConsecutiveSamples,
		))
	}

	if stabilizationWindow := autoscalingScaleDownStabilizationWindow(policy); stabilizationWindow > 0 {
		nextEligibleTime := status.LowPressureSince.Time.Add(stabilizationWindow)
		if time.Now().UTC().Before(nextEligibleTime) {
			message := fmt.Sprintf("Sequential autoscaling scale-down is waiting until %s before step %d of %d can be re-qualified after the previous removal settled", nextEligibleTime.UTC().Format(time.RFC3339), completedSteps+1, plannedSteps)
			setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateBlocked, currentReplicas-1, autoscalingBlockedReasonScaleDownStabilizationPending, "", message)
			setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", message))
			cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", message)
			r.setAutoscalingScaleDownProgressConditions(cluster, "WaitingForAutoscalingScaleDown", message)
			return ctrl.Result{RequeueAfter: minDuration(rolloutPollRequeue, time.Until(nextEligibleTime))}, nil
		}
	}

	if cooldown := autoscalingScaleDownCooldown(policy); cooldown > 0 && cluster.Status.Autoscaling.LastScaleDownTime != nil {
		nextEligibleTime := cluster.Status.Autoscaling.LastScaleDownTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			message := fmt.Sprintf("Sequential autoscaling scale-down is waiting until %s before step %d of %d can be re-qualified after the previous removal settled", nextEligibleTime.UTC().Format(time.RFC3339), completedSteps+1, plannedSteps)
			setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateBlocked, currentReplicas-1, autoscalingBlockedReasonScaleDownCooldownPending, "", message)
			setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("NoScaleDown: %s", message))
			cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", message)
			r.setAutoscalingScaleDownProgressConditions(cluster, "WaitingForAutoscalingScaleDown", message)
			return ctrl.Result{RequeueAfter: minDuration(rolloutPollRequeue, time.Until(nextEligibleTime))}, nil
		}
	}

	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas-1, "", "", fmt.Sprintf("Sequential autoscaling scale-down settled step %d of %d at %d replicas and is now re-qualifying the next removal candidate", completedSteps, plannedSteps, currentReplicas))
	setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, status, fmt.Sprintf("ScaleDown: completed step %d of %d and is re-qualifying the next one-node removal", completedSteps, plannedSteps))
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", fmt.Sprintf("Re-qualifying sequential autoscaling scale-down step %d of %d", completedSteps+1, plannedSteps))
	r.setAutoscalingScaleDownProgressConditions(cluster, "AutoscalingScaleDown", fmt.Sprintf("Sequential autoscaling scale-down completed step %d of %d and is re-qualifying the next safe removal", completedSteps, plannedSteps))
	if r.Recorder != nil {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownRequalified", fmt.Sprintf("Managed autoscaling completed %d/%d planned sequential removals and is re-qualifying the next step", completedSteps, plannedSteps))
	}
	return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func autoscalingScaleDownPreparationDecision(cluster *platformv1alpha1.NiFiCluster, podName, detail string) string {
	if cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked &&
		cluster.Status.Autoscaling.LastScalingDecision != "" {
		return cluster.Status.Autoscaling.LastScalingDecision
	}
	if detail == "" {
		detail = "selected within the bounded one-step model"
	}
	plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(cluster.Status.Autoscaling.Execution)
	if plannedSteps > 1 {
		return autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("ScaleDown: preparing autoscaling candidate pod %s for safe removal as %s because %s", podName, autoscalingScaleDownEpisodeStepMessage(plannedSteps, completedSteps), detail))
	}
	return autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("ScaleDown: preparing autoscaling candidate pod %s for safe removal because %s", podName, detail))
}

func pauseAutoscalingScaleDownForLifecycle(cluster *platformv1alpha1.NiFiCluster, blockedReason, message string) {
	targetReplicas := cluster.Status.Replicas.Desired
	if cluster.Status.Autoscaling.Execution.TargetReplicas != nil {
		targetReplicas = *cluster.Status.Autoscaling.Execution.TargetReplicas
	} else if targetReplicas > 0 {
		targetReplicas--
	}

	phase := cluster.Status.Autoscaling.Execution.Phase
	if phase == "" {
		phase = platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare
	}

	setAutoscalingExecutionStatus(cluster, phase, platformv1alpha1.AutoscalingExecutionStateBlocked, targetReplicas, blockedReason, "", message)
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("NoScaleDown: %s", message))
}

func (r *NiFiClusterReconciler) executeAutoscalingScaleDownStep(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pod corev1.Pod, currentReplicas int32, detail string) (bool, ctrl.Result, error) {
	nextReplicas := currentReplicas - 1
	if err := r.patchTargetReplicas(ctx, target, nextReplicas); err != nil {
		r.markAutoscalingScaleDownFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to %d replicas failed: %v", target.Name, nextReplicas, err))
		return false, ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to %d replicas: %w", target.Namespace, target.Name, nextReplicas, err)
	}

	now := metav1.NewTime(time.Now().UTC())
	if detail == "" {
		detail = "it was the bounded one-step StatefulSet removal candidate"
	}
	plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(cluster.Status.Autoscaling.Execution)
	if plannedSteps <= 0 {
		plannedSteps = 1
	}
	completedSteps++
	stepContext := autoscalingScaleDownEpisodeStepMessage(plannedSteps, completedSteps-1)
	decision := fmt.Sprintf("ScaleDown: reduced target StatefulSet replicas from %d to %d after preparing autoscaling candidate pod %s as %s because %s", currentReplicas, nextReplicas, pod.Name, stepContext, detail)
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	cluster.Status.Replicas.Desired = nextReplicas
	cluster.Status.Autoscaling.LastScaleDownTime = &now
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateRunning, nextReplicas, "", "", fmt.Sprintf("Waiting for autoscaling scale-down step %d of %d to settle at %d replicas", completedSteps, plannedSteps, nextReplicas))
	setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, decision)
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", decision)
	r.setAutoscalingScaleDownProgressConditions(cluster, "AutoscalingScaleDown", fmt.Sprintf("Prepared autoscaling candidate pod %s for safe scale-down and reduced StatefulSet replicas to %d", pod.Name, nextReplicas))
	recordAutoscalingScaleAction("scaled_down")
	if r.Recorder != nil {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownStarted", fmt.Sprintf("Managed autoscaling reduced replicas from %d to %d during step %d of %d after preparing candidate pod %s because %s", currentReplicas, nextReplicas, completedSteps, plannedSteps, pod.Name, detail))
	}
	return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func (r *NiFiClusterReconciler) waitForAutoscalingScaleDownStepToSettle(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (bool, ctrl.Result, error) {
	expectedReplicas := derefInt32(target.Spec.Replicas)
	cluster.Status.Replicas.Desired = expectedReplicas
	plannedSteps, completedSteps := autoscalingScaleDownEpisodeProgress(cluster.Status.Autoscaling.Execution)
	settleMessage := fmt.Sprintf("Waiting for autoscaling scale-down step %d of %d to settle at %d replicas", completedSteps, plannedSteps, expectedReplicas)
	if cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked {
		settleMessage = fmt.Sprintf("Resuming autoscaling scale-down settlement for step %d of %d at %d replicas after a blocked or restarted step", completedSteps, plannedSteps, expectedReplicas)
	}
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateRunning, expectedReplicas, "", "", settleMessage)
	setAutoscalingScaleDownEpisodeProgress(cluster, plannedSteps, completedSteps)

	if podsPendingTermination(pods) || (int32(len(pods)) != expectedReplicas && !statefulSetReplicasSettled(target, expectedReplicas)) {
		message := fmt.Sprintf("Waiting for the previous autoscaling scale-down step to settle at %d replicas before any further decision; current pods: %s", expectedReplicas, podNames(pods))
		if autoscalingScaleDownExecutionTimedOut(cluster, autoscalingScaleDownSettleTimeout(cluster, expectedReplicas)) {
			message = fmt.Sprintf("%s. Autoscaling scale-down remains blocked because pod termination or replica settlement has stalled. Operator action: inspect the terminating pod, PVC finalizers, and Kubernetes events before allowing another scale-down step.", message)
			r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownDrainStalled, "WaitingForAutoscalingScaleDown", message, true)
		} else {
			r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownDrainPending, "WaitingForAutoscalingScaleDown", message, false)
		}
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if cluster.Spec.Safety.RequireClusterHealthy {
		if settleDelay, nextEligibleTime, pending := autoscalingScaleDownSettleDelay(cluster, r.HealthChecker); pending {
			message := fmt.Sprintf("Waiting until %s before sampling post-scale-down health at %d replicas", nextEligibleTime.UTC().Format(time.RFC3339), expectedReplicas)
			r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, "WaitingForScaleDownStabilization", "WaitingForAutoscalingScaleDown", message, false)
			return false, ctrl.Result{RequeueAfter: minDuration(rolloutPollRequeue, settleDelay)}, nil
		}

		if !expectedPodsReady(expectedReplicas, target.Name, pods) {
			message := fmt.Sprintf("Waiting for %d target pods to become Ready before autoscaling can declare the step settled", expectedReplicas)
			if autoscalingScaleDownExecutionTimedOut(cluster, autoscalingScaleDownSettleTimeout(cluster, expectedReplicas)) {
				message = fmt.Sprintf("%s. Autoscaling scale-down remains blocked because post-removal readiness has stalled. Operator action: inspect pod readiness, NiFi logs, and StatefulSet events before allowing another scale-down step.", message)
				r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownReadyPodsStalled, "WaitingForAutoscalingScaleDown", message, true)
			} else {
				r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownReadyPodsPending, "WaitingForAutoscalingScaleDown", message, false)
			}
			return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		healthCtx, cancel := context.WithTimeout(ctx, autoscalingScaleDownHealthCheckTimeout(cluster, expectedReplicas))
		healthResult, err := r.HealthChecker.CheckClusterHealth(healthCtx, cluster, target)
		cancel()
		healthResult = normalizeHibernationClusterHealth(healthResult)
		r.applyClusterHealth(cluster, healthResult)
		if !hibernationClusterHealthy(healthResult) {
			if err == nil {
				err = fmt.Errorf("cluster health gate not yet satisfied: %s", healthResult.Summary())
			}
			message := fmt.Sprintf("Cluster health gate blocked autoscaling scale-down at %d replicas: %v", expectedReplicas, err)
			if autoscalingScaleDownExecutionTimedOut(cluster, autoscalingScaleDownSettleTimeout(cluster, expectedReplicas)) {
				message = fmt.Sprintf("%s. Autoscaling scale-down remains blocked because post-removal drain or cluster recovery has not completed within the bounded settle timeout. Operator action: verify NiFi cluster connectivity and resolve the unhealthy node state before another scale-down step.", message)
				r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownHealthGateTimedOut, "WaitingForAutoscalingScaleDown", message, true)
			} else {
				r.markAutoscalingScaleDownBlocked(cluster, expectedReplicas, autoscalingBlockedReasonScaleDownHealthGateBlocked, "WaitingForAutoscalingScaleDown", message, false)
			}
			return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}
	}

	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	return true, ctrl.Result{}, nil
}

func autoscalingScaleDownHealthCheckTimeout(cluster *platformv1alpha1.NiFiCluster, expectedReplicas int32) time.Duration {
	timeout := 45 * time.Second
	if expectedReplicas > 1 {
		timeout = time.Duration(expectedReplicas) * timeout
	}
	if clusterTimeout := clusterHealthTimeout(cluster); clusterTimeout > 0 && clusterTimeout < timeout {
		return clusterTimeout
	}
	return timeout
}

func autoscalingScaleDownSettleTimeout(cluster *platformv1alpha1.NiFiCluster, expectedReplicas int32) time.Duration {
	return maxDuration(nodePreparationTimeout(cluster), autoscalingScaleDownHealthCheckTimeout(cluster, expectedReplicas))
}

func autoscalingScaleDownExecutionTimedOut(cluster *platformv1alpha1.NiFiCluster, timeout time.Duration) bool {
	startedAt := cluster.Status.Autoscaling.Execution.StartedAt
	if startedAt == nil || timeout <= 0 {
		return false
	}
	return time.Since(startedAt.Time) > timeout
}

func autoscalingScaleDownSettleDelay(cluster *platformv1alpha1.NiFiCluster, checker ClusterHealthChecker) (time.Duration, time.Time, bool) {
	if cluster.Status.Autoscaling.LastScaleDownTime == nil {
		return 0, time.Time{}, false
	}

	requiredStablePolls := hibernationStablePollCount(checker)
	if requiredStablePolls <= 1 {
		return 0, time.Time{}, false
	}

	pollInterval := hibernationPollInterval(checker, clusterHealthTimeout(cluster), requiredStablePolls)
	settleDelay := pollInterval * time.Duration(requiredStablePolls)
	if settleDelay <= 0 {
		return 0, time.Time{}, false
	}

	nextEligibleTime := cluster.Status.Autoscaling.LastScaleDownTime.Time.Add(settleDelay)
	if time.Now().UTC().Before(nextEligibleTime) {
		return settleDelay, nextEligibleTime, true
	}
	return settleDelay, nextEligibleTime, false
}

func statefulSetReplicasSettled(target *appsv1.StatefulSet, expectedReplicas int32) bool {
	return target.Status.Replicas == expectedReplicas &&
		target.Status.ReadyReplicas == expectedReplicas &&
		target.Status.CurrentReplicas == expectedReplicas &&
		target.Status.UpdatedReplicas == expectedReplicas
}

func expectedPodsReady(expectedReplicas int32, statefulSetName string, pods []corev1.Pod) bool {
	if expectedReplicas <= 0 {
		return true
	}

	podByName := make(map[string]corev1.Pod, len(pods))
	for i := range pods {
		podByName[pods[i].Name] = pods[i]
	}

	for ordinal := int32(0); ordinal < expectedReplicas; ordinal++ {
		podName := fmt.Sprintf("%s-%d", statefulSetName, ordinal)
		pod, ok := podByName[podName]
		if !ok {
			return false
		}
		if pod.DeletionTimestamp != nil || !isPodReady(&pod) {
			return false
		}
	}

	return true
}

func minDuration(left, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	if left < right {
		return left
	}
	return right
}

func maxDuration(left, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right <= 0 {
		return left
	}
	if left > right {
		return left
	}
	return right
}

func (r *NiFiClusterReconciler) setAutoscalingScaleDownProgressConditions(cluster *platformv1alpha1.NiFiCluster, reason, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDown",
		Message:            "Cluster availability is temporarily gated while autoscaling removes one controller-selected scale-down candidate",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDown",
		Message:            "No autoscaling scale-down failure is active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiClusterReconciler) blockAutoscalingScaleDownForCandidate(cluster *platformv1alpha1.NiFiCluster, targetReplicas int32, blockedReason, message string) {
	previous := cluster.Status.Autoscaling.Execution
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateBlocked, targetReplicas, blockedReason, "", message)
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("NoScaleDown: %s", message))
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", message)
	r.setAutoscalingScaleDownProgressConditions(cluster, "WaitingForAutoscalingScaleDown", message)
	if r.Recorder != nil && (previous.BlockedReason != blockedReason || previous.Message != message) {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownCandidateBlocked", message)
	}
}

func selectAutoscalingScaleDownCandidate(target *appsv1.StatefulSet, pods []corev1.Pod, currentReplicas int32) autoscalingScaleDownCandidateSelection {
	expectedOrdinal := currentReplicas - 1
	if currentReplicas <= 0 {
		return autoscalingScaleDownCandidateSelection{
			ExpectedPodName: target.Name,
			BlockedReason:   autoscalingBlockedReasonScaleDownCandidateMissing,
			Message:         "waiting for an autoscaling scale-down candidate because the target replica count does not expose a removable pod",
			Detail:          "no removable StatefulSet ordinal is available",
		}
	}

	expectedPodName := fmt.Sprintf("%s-%d", target.Name, expectedOrdinal)
	lowerOrdinals := lowerOrdinalPodNames(pods, expectedOrdinal)
	lowerOrdinalSummary := lowerOrdinalCandidateSummary(lowerOrdinals)
	observedSummary := podNames(pods)
	if observedSummary == "" {
		observedSummary = "none"
	}

	for i := range pods {
		if pods[i].Name != expectedPodName {
			continue
		}
		if pods[i].DeletionTimestamp != nil {
			return autoscalingScaleDownCandidateSelection{
				ExpectedPodName: expectedPodName,
				BlockedReason:   autoscalingBlockedReasonScaleDownCandidateTerminating,
				Message:         fmt.Sprintf("waiting for actual StatefulSet removal candidate pod %s to finish terminating before autoscaling can continue; %s", expectedPodName, lowerOrdinalSummary),
				Detail:          fmt.Sprintf("pod %s is the actual StatefulSet %d->%d removal candidate but is already terminating", expectedPodName, currentReplicas, currentReplicas-1),
			}
		}
		if !isPodReady(&pods[i]) {
			return autoscalingScaleDownCandidateSelection{
				ExpectedPodName: expectedPodName,
				BlockedReason:   autoscalingBlockedReasonScaleDownCandidateNotReady,
				Message:         fmt.Sprintf("waiting for actual StatefulSet removal candidate pod %s to become Ready before autoscaling can continue; %s", expectedPodName, lowerOrdinalSummary),
				Detail:          fmt.Sprintf("pod %s is the actual StatefulSet %d->%d removal candidate but is not Ready", expectedPodName, currentReplicas, currentReplicas-1),
			}
		}
		return autoscalingScaleDownCandidateSelection{
			Pod:             pods[i],
			Selected:        true,
			ExpectedPodName: expectedPodName,
			Detail:          fmt.Sprintf("pod %s is the actual StatefulSet %d->%d removal candidate and is Ready; %s", expectedPodName, currentReplicas, currentReplicas-1, lowerOrdinalSummary),
		}
	}

	return autoscalingScaleDownCandidateSelection{
		ExpectedPodName: expectedPodName,
		BlockedReason:   autoscalingBlockedReasonScaleDownCandidateMissing,
		Message:         fmt.Sprintf("waiting for actual StatefulSet removal candidate pod %s to appear before autoscaling can continue; observed pods: %s. %s", expectedPodName, observedSummary, lowerOrdinalSummary),
		Detail:          fmt.Sprintf("pod %s is the actual StatefulSet %d->%d removal candidate but is missing from the live pod list", expectedPodName, currentReplicas, currentReplicas-1),
	}
}

func lowerOrdinalPodNames(pods []corev1.Pod, expectedOrdinal int32) []string {
	names := make([]string, 0, len(pods))
	for i := range pods {
		ordinal, ok := podOrdinal(&pods[i])
		if !ok || int32(ordinal) >= expectedOrdinal {
			continue
		}
		names = append(names, pods[i].Name)
	}
	sort.Strings(names)
	return names
}

func lowerOrdinalCandidateSummary(names []string) string {
	if len(names) == 0 {
		return "no lower ordinal fallback is available within the bounded one-step StatefulSet model"
	}
	return fmt.Sprintf("lower ordinals %s remain in service because one-step StatefulSet scale-down cannot safely switch deletion targets", strings.Join(names, ","))
}

func (r *NiFiClusterReconciler) markAutoscalingScaleDownBlocked(cluster *platformv1alpha1.NiFiCluster, targetReplicas int32, blockedReason, progressReason, message string, degraded bool) {
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateBlocked, targetReplicas, blockedReason, "", message)
	r.setAutoscalingScaleDownProgressConditions(cluster, progressReason, message)
	if degraded {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             blockedReason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
	}
	cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("NoScaleDown: %s", message))
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", message)
}

func (r *NiFiClusterReconciler) markAutoscalingScaleDownFailure(cluster *platformv1alpha1.NiFiCluster, message string) {
	targetReplicas := cluster.Status.Replicas.Desired
	if cluster.Status.Autoscaling.Execution.TargetReplicas != nil {
		targetReplicas = *cluster.Status.Autoscaling.Execution.TargetReplicas
	}
	setAutoscalingExecutionStatus(cluster, cluster.Status.Autoscaling.Execution.Phase, platformv1alpha1.AutoscalingExecutionStateFailed, targetReplicas, "", "AutoscalingScaleDownFailed", message)
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDownFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleDownFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "AutoscalingScaleDownFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = failedOperation("AutoscalingScaleDown", message)
	recordAutoscalingScaleAction("scale_down_failed")
	if r.Recorder != nil {
		r.Recorder.Event(cluster, corev1.EventTypeWarning, "AutoscalingScaleDownFailed", message)
	}
}
