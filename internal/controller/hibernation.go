package controller

import (
	"context"
	"fmt"

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

	currentReplicas := derefInt32(target.Spec.Replicas)
	if currentReplicas > 0 {
		if cluster.Status.Hibernation.LastRunningReplicas == 0 {
			cluster.Status.Hibernation.LastRunningReplicas = currentReplicas
		}

		if cluster.Spec.Safety.RequireClusterHealthy {
			if err := r.HealthChecker.WaitForPodsReady(ctx, target, podReadyTimeout(cluster)); err != nil {
				r.markHibernationHealthBlocked(cluster, fmt.Sprintf("Waiting for target pods to become Ready before scaling to zero: %v", err))
				return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
			}

			healthResult, err := r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
			r.applyClusterHealth(cluster, healthResult)
			if err != nil {
				r.markHibernationHealthBlocked(cluster, fmt.Sprintf("Cluster health gate blocked hibernation: %v", err))
				return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
			}
		}

		if err := r.patchTargetReplicas(ctx, target, 0); err != nil {
			r.markHibernationFailure(cluster, fmt.Sprintf("Scale target StatefulSet %q to zero failed: %v", target.Name, err))
			return ctrl.Result{}, fmt.Errorf("scale StatefulSet %s/%s to zero: %w", target.Namespace, target.Name, err)
		}

		cluster.Status.Replicas.Desired = 0
		cluster.Status.LastOperation = runningOperation("Hibernation", fmt.Sprintf("Scaled StatefulSet %q to 0 replicas; waiting for pods to terminate", target.Name))
		r.setHibernationProgressConditions(cluster, "ScalingDown", "Managed hibernation scaled the target StatefulSet to 0 replicas and is waiting for the remaining pods to terminate")
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
		Message:            "Cluster is intentionally scaling down to zero replicas",
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
	return hibernationFallbackReplicas
}

func int32ptr(value int32) *int32 {
	return &value
}
