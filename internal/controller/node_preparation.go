package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

func (r *NiFiClusterReconciler) preparePodForRestart(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod) (bool, ctrl.Result, error) {
	return r.preparePodForOperation(ctx, cluster, target, pods, pod, platformv1alpha1.NodeOperationPurposeRestart)
}

func (r *NiFiClusterReconciler) preparePodForHibernation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod) (bool, ctrl.Result, error) {
	return r.preparePodForOperation(ctx, cluster, target, pods, pod, platformv1alpha1.NodeOperationPurposeHibernation)
}

func (r *NiFiClusterReconciler) preparePodForOperation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose) (bool, ctrl.Result, error) {
	clearNodeOperationIfPodMissing(cluster, pods)

	result, err := r.NodeManager.PreparePodForOperation(ctx, cluster, target, pods, pod, purpose, cluster.Status.NodeOperation, nodePreparationTimeout(cluster))
	if err != nil {
		r.markNodePreparationBlocked(cluster, purpose, fmt.Sprintf("Waiting for NiFi node preparation: %v", err))
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	cluster.Status.NodeOperation = result.Operation
	if result.Ready {
		cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
		return true, ctrl.Result{}, nil
	}

	if result.TimedOut {
		r.markNodePreparationTimedOut(cluster, purpose, result.Message)
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	r.markNodePreparationProgress(cluster, purpose, result.Message)
	if result.RequeueNow {
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}
	return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func clearNodeOperationIfPodMissing(cluster *platformv1alpha1.NiFiCluster, pods []corev1.Pod) {
	if cluster.Status.NodeOperation.PodName == "" {
		return
	}

	for i := range pods {
		if pods[i].Name != cluster.Status.NodeOperation.PodName {
			continue
		}
		if cluster.Status.NodeOperation.PodUID != "" && string(pods[i].UID) != cluster.Status.NodeOperation.PodUID {
			cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
			return
		}
		if cluster.Status.NodeOperation.PodUID == "" || string(pods[i].UID) == cluster.Status.NodeOperation.PodUID {
			return
		}
	}

	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
}

func findNodeOperationPod(pods []corev1.Pod, current platformv1alpha1.NodeOperationStatus, purpose platformv1alpha1.NodeOperationPurpose) (corev1.Pod, bool) {
	if current.PodName == "" || current.Purpose != purpose {
		return corev1.Pod{}, false
	}

	for i := range pods {
		if pods[i].Name != current.PodName {
			continue
		}
		if current.PodUID != "" && string(pods[i].UID) != current.PodUID {
			return corev1.Pod{}, false
		}
		if current.PodUID == "" || string(pods[i].UID) == current.PodUID {
			return pods[i], true
		}
	}

	return corev1.Pod{}, false
}

func (r *NiFiClusterReconciler) markNodePreparationProgress(cluster *platformv1alpha1.NiFiCluster, purpose platformv1alpha1.NodeOperationPurpose, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForNodePreparation",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             progressingReasonForNodePreparation(purpose),
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "NodePreparationInProgress",
		Message:            "No failure is active while NiFi prepares the target node",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation(string(purpose), message)
}

func (r *NiFiClusterReconciler) markNodePreparationTimedOut(cluster *platformv1alpha1.NiFiCluster, purpose platformv1alpha1.NodeOperationPurpose, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "NodePreparationTimedOut",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             progressingReasonForNodePreparation(purpose),
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "NodePreparationTimedOut",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation(string(purpose), message)
}

func (r *NiFiClusterReconciler) markNodePreparationBlocked(cluster *platformv1alpha1.NiFiCluster, purpose platformv1alpha1.NodeOperationPurpose, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForNodePreparation",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             progressingReasonForNodePreparation(purpose),
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "NodePreparationRetrying",
		Message:            "NiFi node preparation is retrying after an API or connectivity error",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation(string(purpose), message)
}

func progressingReasonForNodePreparation(purpose platformv1alpha1.NodeOperationPurpose) string {
	if purpose == platformv1alpha1.NodeOperationPurposeHibernation {
		return "PreparingNodeForHibernation"
	}
	return "PreparingNodeForRestart"
}

func nodePreparationTimeout(cluster *platformv1alpha1.NiFiCluster) time.Duration {
	if cluster.Spec.Hibernation.OffloadTimeout.Duration > 0 {
		return cluster.Spec.Hibernation.OffloadTimeout.Duration
	}
	return defaultNodePreparationTimeout
}
