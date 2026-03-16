package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const (
	autoscalingBlockedReasonScaleDownDrainPending       = "ScaleDownDrainPending"
	autoscalingBlockedReasonScaleDownDrainStalled       = "ScaleDownDrainStalled"
	autoscalingBlockedReasonScaleDownReadyPodsPending   = "ScaleDownReadyPodsPending"
	autoscalingBlockedReasonScaleDownReadyPodsStalled   = "ScaleDownReadyPodsStalled"
	autoscalingBlockedReasonScaleDownHealthGateBlocked  = "ScaleDownHealthGateBlocked"
	autoscalingBlockedReasonScaleDownHealthGateTimedOut = "ScaleDownHealthGateTimedOut"
)

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
		cluster.Status.Autoscaling.LastScalingDecision = "NoScale: autoscaling is not in enforced mode"
		return false, ctrl.Result{}, nil
	}

	if !policy.ScaleDown.Enabled {
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleDown: scale-down is not enabled"
		return false, ctrl.Result{}, nil
	}
	if status.RecommendedReplicas == nil {
		cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: recommendation is unavailable because %s", status.Reason)
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
		cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: minimum replicas %d are already satisfied", minReplicas)
		return false, ctrl.Result{}, nil
	}
	if status.LowPressureSince == nil {
		if reason := autoscalingLowPressureBlockedReason(samples); reason != "" {
			cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: %s", reason)
		} else {
			cluster.Status.Autoscaling.LastScalingDecision = "NoScaleDown: low pressure is not currently observed"
		}
		return false, ctrl.Result{}, nil
	}
	if !autoscalingLowPressureRequirementMet(status.LowPressure) {
		cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf(
			"NoScaleDown: low pressure needs %d/%d consecutive zero-backlog evaluations before any scale-down step",
			status.LowPressure.ConsecutiveSamples,
			status.LowPressure.RequiredConsecutiveSamples,
		)
		return false, ctrl.Result{}, nil
	}

	stabilizationWindow := autoscalingScaleDownStabilizationWindow(policy)
	if stabilizationWindow > 0 {
		nextEligibleTime := status.LowPressureSince.Time.Add(stabilizationWindow)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: low pressure must remain stable until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			return false, ctrl.Result{}, nil
		}
	}

	cooldown := autoscalingScaleDownCooldown(policy)
	if executionState.lastScaleDownTime != nil && cooldown > 0 {
		nextEligibleTime := executionState.lastScaleDownTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			return false, ctrl.Result{}, nil
		}
	}

	targetPod, ok := highestOrdinalPod(pods)
	if !ok {
		message := "Waiting for the highest ordinal pod to appear before autoscaling scale-down can continue"
		setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateBlocked, currentReplicas-1, "WaitingForHighestOrdinalPod", "", message)
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleDown: waiting for the highest ordinal pod to appear"
		cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", message)
		r.setAutoscalingScaleDownProgressConditions(cluster, "WaitingForAutoscalingScaleDown", message)
		return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	prepared, result, err := r.preparePodForScaleDown(ctx, cluster, target, pods, targetPod)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if !prepared {
		if cluster.Status.Autoscaling.Execution.State == "" || cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateRunning {
			setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, platformv1alpha1.AutoscalingExecutionStateRunning, currentReplicas-1, "", "", fmt.Sprintf("Preparing pod %s for safe autoscaling scale-down", targetPod.Name))
		}
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingScaleDownPreparationDecision(cluster, targetPod.Name)
		return true, result, nil
	}

	return r.executeAutoscalingScaleDownStep(ctx, cluster, target, targetPod, currentReplicas)
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
			cluster.Status.Autoscaling.LastScalingDecision = autoscalingScaleDownPreparationDecision(cluster, currentNodeOpPod.Name)
			return result, nil
		}
		handled, result, err := r.executeAutoscalingScaleDownStep(ctx, cluster, target, currentNodeOpPod, currentReplicas)
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
	clearAutoscalingExecution(cluster)
	cluster.Status.LastOperation = succeededOperation("AutoscalingScaleDown", fmt.Sprintf("Managed autoscaling safely settled at %d replicas", currentReplicas))
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
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownCompleted", fmt.Sprintf("Managed autoscaling settled safely at %d replicas", currentReplicas))
	}
	return steadyStateReconcileResult(cluster), nil
}

func autoscalingScaleDownPreparationDecision(cluster *platformv1alpha1.NiFiCluster, podName string) string {
	if cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked &&
		cluster.Status.Autoscaling.LastScalingDecision != "" {
		return cluster.Status.Autoscaling.LastScalingDecision
	}
	return fmt.Sprintf("ScaleDown: preparing pod %s for safe removal", podName)
}

func (r *NiFiClusterReconciler) executeAutoscalingScaleDownStep(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pod corev1.Pod, currentReplicas int32) (bool, ctrl.Result, error) {
	nextReplicas := currentReplicas - 1
	if err := r.patchTargetReplicas(ctx, target, nextReplicas); err != nil {
		r.markAutoscalingScaleDownFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to %d replicas failed: %v", target.Name, nextReplicas, err))
		return false, ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to %d replicas: %w", target.Namespace, target.Name, nextReplicas, err)
	}

	now := metav1.NewTime(time.Now().UTC())
	decision := fmt.Sprintf("ScaleDown: reduced target StatefulSet replicas from %d to %d after preparing pod %s", currentReplicas, nextReplicas, pod.Name)
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	cluster.Status.Replicas.Desired = nextReplicas
	cluster.Status.Autoscaling.LastScalingDecision = decision
	cluster.Status.Autoscaling.LastScaleDownTime = &now
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateRunning, nextReplicas, "", "", fmt.Sprintf("Waiting for the autoscaling scale-down step to settle at %d replicas", nextReplicas))
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", fmt.Sprintf("%s because sustained low pressure made the higher ordinal removable", decision))
	r.setAutoscalingScaleDownProgressConditions(cluster, "AutoscalingScaleDown", fmt.Sprintf("Prepared pod %s for autoscaling scale-down and reduced StatefulSet replicas to %d", pod.Name, nextReplicas))
	recordAutoscalingScaleAction("scaled_down")
	if r.Recorder != nil {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "AutoscalingScaleDownStarted", fmt.Sprintf("Managed autoscaling reduced replicas from %d to %d after preparing pod %s", currentReplicas, nextReplicas, pod.Name))
	}
	return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func (r *NiFiClusterReconciler) waitForAutoscalingScaleDownStepToSettle(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (bool, ctrl.Result, error) {
	expectedReplicas := derefInt32(target.Spec.Replicas)
	cluster.Status.Replicas.Desired = expectedReplicas
	settleMessage := fmt.Sprintf("Waiting for the autoscaling scale-down step to settle at %d replicas", expectedReplicas)
	if cluster.Status.Autoscaling.Execution.State == platformv1alpha1.AutoscalingExecutionStateBlocked {
		settleMessage = fmt.Sprintf("Resuming autoscaling scale-down settlement at %d replicas after a blocked or restarted step", expectedReplicas)
	}
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle, platformv1alpha1.AutoscalingExecutionStateRunning, expectedReplicas, "", "", settleMessage)

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
		Message:            "Cluster availability is temporarily gated while autoscaling removes one highest ordinal node",
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
	cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleDown: %s", message)
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
