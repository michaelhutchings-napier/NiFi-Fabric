package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

const hibernationFallbackReplicas int32 = 1

func restoreInProgress(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) bool {
	if cluster.Spec.DesiredState != platformv1alpha1.DesiredStateRunning {
		return false
	}
	if derefInt32(target.Spec.Replicas) == 0 {
		return true
	}
	condition := cluster.GetCondition(platformv1alpha1.ConditionHibernated)
	return condition != nil && condition.Status == metav1.ConditionFalse && condition.Reason == "Restoring"
}

func (r *NiFiClusterReconciler) reconcileHibernation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (ctrl.Result, error) {
	if cluster.Status.Rollout.Trigger != "" {
		r.markHibernationWaitingForRollout(cluster)
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	liveTarget, livePods, err := r.liveHibernationTargetState(ctx, target)
	if err != nil {
		return ctrl.Result{}, err
	}
	target = liveTarget
	pods = livePods

	currentReplicas := derefInt32(target.Spec.Replicas)
	if currentReplicas > 0 {
		if cluster.Status.Hibernation.LastRunningReplicas == 0 {
			cluster.Status.Hibernation.LastRunningReplicas = currentReplicas
		}

		clearNodeOperationIfPodMissing(cluster, pods)

		if currentNodeOpPod, ok := findNodeOperationPod(pods, cluster.Status.NodeOperation, platformv1alpha1.NodeOperationPurposeHibernation); ok {
			prepared, result, err := r.preparePodForHibernation(ctx, cluster, target, pods, currentNodeOpPod)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !prepared {
				return result, nil
			}

			nextReplicas := currentReplicas - 1
			if err := r.patchTargetReplicas(ctx, target, nextReplicas); err != nil {
				r.markHibernationFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to %d replicas failed: %v", target.Name, nextReplicas, err))
				return ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to %d replicas: %w", target.Namespace, target.Name, nextReplicas, err)
			}

			cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
			cluster.Status.Replicas.Desired = nextReplicas
			cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Scaled StatefulSet %q down to %d replicas after preparing pod %s for hibernation", target.Name, nextReplicas, currentNodeOpPod.Name))
			r.setHibernationProgressConditions(cluster, "ScalingDown", fmt.Sprintf("Prepared pod %s for hibernation and reduced StatefulSet replicas to %d", currentNodeOpPod.Name, nextReplicas))
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		settled, result, err := r.waitForHibernationStepToSettle(ctx, cluster, target, pods)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !settled {
			return result, nil
		}

		if currentReplicas == 1 {
			if err := r.patchTargetReplicas(ctx, target, 0); err != nil {
				r.markHibernationFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to 0 replicas failed: %v", target.Name, err))
				return ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to 0 replicas: %w", target.Namespace, target.Name, err)
			}

			cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
			cluster.Status.Replicas.Desired = 0
			cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Scaled StatefulSet %q down to 0 replicas after the final hibernation step", target.Name))
			r.setHibernationProgressConditions(cluster, "ScalingDown", fmt.Sprintf("Scaled the final NiFi pod down to zero replicas to complete hibernation for StatefulSet %q", target.Name))
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		targetPod, ok := highestOrdinalPod(pods)
		if !ok {
			cluster.Status.LastOperation = runningOperation("Hibernation", "Waiting for the highest ordinal pod to appear before hibernation can continue")
			r.setHibernationProgressConditions(cluster, "WaitingForScaleDown", "Waiting for the highest ordinal pod to appear before hibernation can continue")
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		prepared, result, err := r.preparePodForHibernation(ctx, cluster, target, pods, targetPod)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !prepared {
			return result, nil
		}

		nextReplicas := currentReplicas - 1
		if err := r.patchTargetReplicas(ctx, target, nextReplicas); err != nil {
			r.markHibernationFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to %d replicas failed: %v", target.Name, nextReplicas, err))
			return ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to %d replicas: %w", target.Namespace, target.Name, nextReplicas, err)
		}

		cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
		cluster.Status.Replicas.Desired = nextReplicas
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Scaled StatefulSet %q down to %d replicas after preparing pod %s for hibernation", target.Name, nextReplicas, targetPod.Name))
		r.setHibernationProgressConditions(cluster, "ScalingDown", fmt.Sprintf("Prepared pod %s for hibernation and reduced StatefulSet replicas to %d", targetPod.Name, nextReplicas))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if len(pods) > 0 {
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Waiting for pods to terminate during hibernation: %s", podNames(pods)))
		r.setHibernationProgressConditions(cluster, "WaitingForScaleDown", fmt.Sprintf("Waiting for pods to terminate during hibernation: %s", podNames(pods)))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	cluster.Status.Replicas = platformv1alpha1.ReplicaStatus{}
	cluster.Status.ClusterNodes = platformv1alpha1.ClusterNodesStatus{}
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	clearTLSObservation(cluster)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernated",
		Message:            "The target StatefulSet is scaled to zero while hibernated",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernated",
		Message:            "No hibernation work is in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernated",
		Message:            "No failure is active while the cluster is intentionally hibernated",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionTrue,
		Reason:             "Hibernated",
		Message:            fmt.Sprintf("Cluster is hibernated and can restore to %d replicas", restoreReplicaFallback(cluster)),
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = succeededOperation("Hibernation", fmt.Sprintf("Cluster is hibernated with PVCs preserved; restore target is %d replicas", restoreReplicaFallback(cluster)))
	return ctrl.Result{}, nil
}

func (r *NiFiClusterReconciler) liveHibernationTargetState(ctx context.Context, target *appsv1.StatefulSet) (*appsv1.StatefulSet, []corev1.Pod, error) {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}

	freshTarget := &appsv1.StatefulSet{}
	if err := reader.Get(ctx, client.ObjectKeyFromObject(target), freshTarget); err != nil {
		return nil, nil, fmt.Errorf("refresh StatefulSet %s/%s for hibernation: %w", target.Namespace, target.Name, err)
	}

	pods, err := listTargetPodsWithReader(ctx, reader, freshTarget)
	if err != nil {
		return nil, nil, err
	}

	return freshTarget, pods, nil
}

func (r *NiFiClusterReconciler) waitForHibernationStepToSettle(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) (bool, ctrl.Result, error) {
	expectedReplicas := derefInt32(target.Spec.Replicas)
	cluster.Status.Replicas.Desired = expectedReplicas

	if podsPendingTermination(pods) || int32(len(pods)) != expectedReplicas {
		message := fmt.Sprintf("Waiting for the previous hibernation scale-down step to settle at %d replicas before removing the next node; current pods: %s", expectedReplicas, podNames(pods))
		cluster.Status.LastOperation = runningOperation("Hibernation", message)
		r.setHibernationProgressConditions(cluster, "WaitingForScaleDown", message)
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if cluster.Spec.Safety.RequireClusterHealthy {
		if err := r.HealthChecker.WaitForPodsReady(ctx, target, podReadyTimeout(cluster)); err != nil {
			r.markHibernationHealthBlocked(cluster, fmt.Sprintf("Waiting for %d target pods to become Ready before the next hibernation step: %v", expectedReplicas, err))
			return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		healthResult, err := r.waitForHibernationClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
		r.applyClusterHealth(cluster, healthResult)
		if err != nil {
			r.markHibernationHealthBlocked(cluster, fmt.Sprintf("Cluster health gate blocked hibernation at %d replicas: %v", expectedReplicas, err))
			return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}
	}

	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	return true, ctrl.Result{}, nil
}

func (r *NiFiClusterReconciler) waitForHibernationClusterHealthy(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, timeout time.Duration) (ClusterHealthResult, error) {
	if timeout <= 0 {
		timeout = defaultClusterHealthTimeout
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	requiredStablePolls := hibernationStablePollCount(r.HealthChecker)
	pollInterval := hibernationPollInterval(r.HealthChecker, timeout, requiredStablePolls)
	stablePolls := 0
	lastResult := ClusterHealthResult{ExpectedReplicas: derefInt32(target.Spec.Replicas)}

	for {
		result, err := r.HealthChecker.CheckClusterHealth(deadlineCtx, cluster, target)
		result = normalizeHibernationClusterHealth(result)
		lastResult = result

		if hibernationClusterHealthy(result) {
			stablePolls++
			lastResult.StablePolls = stablePolls
			if stablePolls >= requiredStablePolls {
				return lastResult, nil
			}
		} else {
			stablePolls = 0
			lastResult.StablePolls = 0
			if err == nil && result.ExpectedReplicas > 0 {
				err = fmt.Errorf("cluster health gate not yet satisfied: %s", lastResult.Summary())
			}
		}

		if deadlineCtx.Err() != nil {
			return lastResult, fmt.Errorf("timed out waiting for stable hibernation cluster health: %s", lastResult.Summary())
		}
		if err := sleepWithContext(deadlineCtx, pollInterval); err != nil {
			return lastResult, fmt.Errorf("timed out waiting for stable hibernation cluster health: %s", lastResult.Summary())
		}
	}
}

func hibernationStablePollCount(checker ClusterHealthChecker) int {
	if liveChecker, ok := checker.(*LiveClusterHealthChecker); ok {
		return liveChecker.requiredStablePolls()
	}
	return defaultStableHealthPollCount
}

func hibernationPollInterval(checker ClusterHealthChecker, timeout time.Duration, stablePolls int) time.Duration {
	if liveChecker, ok := checker.(*LiveClusterHealthChecker); ok {
		return liveChecker.pollInterval()
	}
	if stablePolls <= 0 {
		stablePolls = defaultStableHealthPollCount
	}
	if timeout <= 0 {
		return defaultHealthPollInterval
	}

	interval := timeout / time.Duration(stablePolls+1)
	if interval <= 0 {
		return time.Millisecond
	}
	if interval > defaultHealthPollInterval {
		return defaultHealthPollInterval
	}
	return interval
}

func normalizeHibernationClusterHealth(result ClusterHealthResult) ClusterHealthResult {
	if result.ExpectedReplicas <= 0 {
		return result
	}

	converged := int32(0)
	for _, pod := range result.Pods {
		if hibernationPodHealthy(result.ExpectedReplicas, pod) {
			converged++
		}
	}
	result.ConvergedPods = converged
	return result
}

func hibernationClusterHealthy(result ClusterHealthResult) bool {
	if result.ExpectedReplicas <= 0 {
		return true
	}
	if result.ReadyPods != result.ExpectedReplicas || result.ReachablePods != result.ExpectedReplicas || int32(len(result.Pods)) != result.ExpectedReplicas {
		return false
	}

	for _, pod := range result.Pods {
		if !hibernationPodHealthy(result.ExpectedReplicas, pod) {
			return false
		}
	}

	return true
}

func hibernationPodHealthy(expectedReplicas int32, pod PodHealth) bool {
	return pod.Ready &&
		pod.APIReachable &&
		pod.Clustered &&
		pod.ConnectedToCluster &&
		pod.ConnectedNodeCount == expectedReplicas &&
		pod.TotalNodeCount >= expectedReplicas
}

func (r *NiFiClusterReconciler) reconcileRestore(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (ctrl.Result, error) {
	restoreReplicas := restoreReplicaFallback(cluster)
	currentReplicas := derefInt32(target.Spec.Replicas)
	if currentReplicas < restoreReplicas {
		if err := r.patchTargetReplicas(ctx, target, restoreReplicas); err != nil {
			r.markHibernationFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to %d replicas failed: %v", target.Name, restoreReplicas, err))
			return ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to %d replicas: %w", target.Namespace, target.Name, restoreReplicas, err)
		}

		cluster.Status.Replicas.Desired = restoreReplicas
		r.setRestoreProgressConditions(cluster, restoreReplicas, "ScalingUp", fmt.Sprintf("Restoring StatefulSet %q to %d replicas", target.Name, restoreReplicas))
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Restoring cluster from hibernation to %d replicas", restoreReplicas))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	r.setRestoreProgressConditions(cluster, restoreReplicas, "WaitingForClusterHealth", fmt.Sprintf("Waiting for pod readiness and cluster convergence while restoring to %d replicas", restoreReplicas))
	cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Waiting for pod readiness and cluster convergence while restoring to %d replicas", restoreReplicas))

	if err := r.HealthChecker.WaitForPodsReady(ctx, target, podReadyTimeout(cluster)); err != nil {
		r.setRestoreProgressConditions(cluster, restoreReplicas, "WaitingForPodsReady", fmt.Sprintf("Waiting for restored pods to become Ready: %v", err))
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Waiting for restored pods to become Ready: %v", err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	healthResult, err := r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		r.setRestoreProgressConditions(cluster, restoreReplicas, "WaitingForClusterHealth", fmt.Sprintf("Waiting for restored cluster convergence: %v", err))
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Waiting for restored cluster convergence: %v", err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RestoreCompleted",
		Message:            fmt.Sprintf("Cluster restored from hibernation and is healthy at %d replicas", restoreReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RestoreCompleted",
		Message:            "Restore is complete and no further hibernation work is in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "No degradation detected",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	cluster.Status.LastOperation = succeededOperation("Hibernation", fmt.Sprintf("Restored cluster from hibernation to %d replicas and regained stable health", restoreReplicas))
	return ctrl.Result{}, nil
}

func (r *NiFiClusterReconciler) patchTargetReplicas(ctx context.Context, target *appsv1.StatefulSet, replicas int32) error {
	original := target.DeepCopy()
	target.Spec.Replicas = int32ptr(replicas)
	return r.Patch(ctx, target, client.MergeFrom(original))
}

func (r *NiFiClusterReconciler) setHibernationProgressConditions(cluster *platformv1alpha1.NiFiCluster, reason, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernating",
		Message:            "Cluster is intentionally scaling down one ordinal at a time toward zero replicas",
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
		Reason:             "Hibernating",
		Message:            "No failure is active while the cluster is being hibernated",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernating",
		Message:            "Managed hibernation is in progress",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiClusterReconciler) setRestoreProgressConditions(cluster *platformv1alpha1.NiFiCluster, restoreReplicas int32, reason, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Restoring",
		Message:            fmt.Sprintf("Waiting for the cluster to restore to %d replicas", restoreReplicas),
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
		Reason:             "Restoring",
		Message:            "No failure is active while the cluster is restoring from hibernation",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Restoring",
		Message:            fmt.Sprintf("Cluster is restoring toward %d replicas", restoreReplicas),
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiClusterReconciler) markHibernationWaitingForRollout(cluster *platformv1alpha1.NiFiCluster) {
	r.setHibernationProgressConditions(cluster, "WaitingForExistingRollout", "Hibernation is waiting for the active managed rollout to finish")
	cluster.Status.LastOperation = runningOperation("Hibernation", "Waiting for the active managed rollout to finish before hibernating")
}

func (r *NiFiClusterReconciler) markHibernationHealthBlocked(cluster *platformv1alpha1.NiFiCluster, message string) {
	r.setHibernationProgressConditions(cluster, "WaitingForClusterHealth", message)
	cluster.Status.LastOperation = runningOperation("Hibernation", message)
}

func (r *NiFiClusterReconciler) markHibernationFailure(cluster *platformv1alpha1.NiFiCluster, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "HibernationFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "HibernationFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "HibernationFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = failedOperation("Hibernation", message)
}

func restoreReplicaFallback(cluster *platformv1alpha1.NiFiCluster) int32 {
	if cluster.Status.Hibernation.LastRunningReplicas > 0 {
		return cluster.Status.Hibernation.LastRunningReplicas
	}
	if cluster.Status.Hibernation.BaselineReplicas > 0 {
		return cluster.Status.Hibernation.BaselineReplicas
	}
	return hibernationFallbackReplicas
}

func int32ptr(value int32) *int32 {
	return &value
}
