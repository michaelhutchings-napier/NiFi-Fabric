package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const (
	autoscalingBlockedReasonScaleDownNodePreparationRetrying = "ScaleDownNodePreparationRetrying"
	autoscalingBlockedReasonScaleDownNodePreparationTimedOut = "ScaleDownNodePreparationTimedOut"
	autoscalingBlockedReasonScaleDownDisconnectRetrying      = "ScaleDownDisconnectRetrying"
	autoscalingBlockedReasonScaleDownOffloadRetrying         = "ScaleDownOffloadRetrying"
	autoscalingBlockedReasonScaleDownDisconnectTimedOut      = "ScaleDownDisconnectTimedOut"
	autoscalingBlockedReasonScaleDownOffloadTimedOut         = "ScaleDownOffloadTimedOut"
)

func (r *NiFiClusterReconciler) preparePodForRestart(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod) (bool, ctrl.Result, error) {
	return r.preparePodForOperation(ctx, cluster, target, pods, pod, platformv1alpha1.NodeOperationPurposeRestart)
}

func (r *NiFiClusterReconciler) preparePodForHibernation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod) (bool, ctrl.Result, error) {
	return r.preparePodForOperation(ctx, cluster, target, pods, pod, platformv1alpha1.NodeOperationPurposeHibernation)
}

func (r *NiFiClusterReconciler) preparePodForScaleDown(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod) (bool, ctrl.Result, error) {
	return r.preparePodForOperation(ctx, cluster, target, pods, pod, platformv1alpha1.NodeOperationPurposeScaleDown)
}

func (r *NiFiClusterReconciler) preparePodForOperation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose) (bool, ctrl.Result, error) {
	clearNodeOperationIfPodMissing(cluster, pods)

	result, err := r.NodeManager.PreparePodForOperation(ctx, cluster, target, pods, pod, purpose, cluster.Status.NodeOperation, nodePreparationTimeout(cluster))
	if err != nil {
		message := fmt.Sprintf("Waiting for NiFi node preparation: %v", err)
		if purpose == platformv1alpha1.NodeOperationPurposeScaleDown {
			message = autoscalingScaleDownNodePreparationGuidance(cluster.Status.NodeOperation, message, true)
		}
		r.updateAutoscalingExecutionForNodePreparation(cluster, purpose, platformv1alpha1.AutoscalingExecutionStateBlocked, autoscalingScaleDownNodePreparationRetryReason(cluster.Status.NodeOperation), "", message)
		r.markNodePreparationBlocked(cluster, purpose, message)
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	cluster.Status.NodeOperation = result.Operation
	if result.Ready {
		cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
		return true, ctrl.Result{}, nil
	}

	if result.TimedOut {
		message := result.Message
		if purpose == platformv1alpha1.NodeOperationPurposeScaleDown {
			message = autoscalingScaleDownNodePreparationGuidance(result.Operation, result.Message, false)
		}
		r.updateAutoscalingExecutionForNodePreparation(cluster, purpose, platformv1alpha1.AutoscalingExecutionStateBlocked, autoscalingScaleDownNodePreparationTimeoutReason(result.Operation), "", message)
		r.markNodePreparationTimedOut(cluster, purpose, message)
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if purpose == platformv1alpha1.NodeOperationPurposeScaleDown && result.RequeueNow && strings.HasPrefix(strings.ToLower(result.Message), "retrying ") {
		message := autoscalingScaleDownNodePreparationGuidance(result.Operation, result.Message, true)
		r.updateAutoscalingExecutionForNodePreparation(cluster, purpose, platformv1alpha1.AutoscalingExecutionStateBlocked, autoscalingScaleDownNodePreparationRetryReason(result.Operation), "", message)
		r.markNodePreparationBlocked(cluster, purpose, message)
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	r.markNodePreparationProgress(cluster, purpose, result.Message)
	r.updateAutoscalingExecutionForNodePreparation(cluster, purpose, platformv1alpha1.AutoscalingExecutionStateRunning, "", "", result.Message)
	if result.RequeueNow {
		return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}
	return false, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func (r *NiFiClusterReconciler) updateAutoscalingExecutionForNodePreparation(cluster *platformv1alpha1.NiFiCluster, purpose platformv1alpha1.NodeOperationPurpose, state platformv1alpha1.AutoscalingExecutionState, blockedReason, failureReason, message string) {
	if purpose != platformv1alpha1.NodeOperationPurposeScaleDown {
		return
	}

	targetReplicas := cluster.Status.Replicas.Desired - 1
	if targetReplicas < 0 {
		targetReplicas = 0
	}
	if cluster.Status.Autoscaling.Execution.TargetReplicas != nil {
		targetReplicas = *cluster.Status.Autoscaling.Execution.TargetReplicas
	}
	setAutoscalingExecutionStatus(cluster, platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare, state, targetReplicas, blockedReason, failureReason, message)
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
	recordNodePreparationOutcome(purpose, "timed_out")
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
	if purpose == platformv1alpha1.NodeOperationPurposeScaleDown {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("NoScaleDown: %s", message))
	}
}

func (r *NiFiClusterReconciler) markNodePreparationBlocked(cluster *platformv1alpha1.NiFiCluster, purpose platformv1alpha1.NodeOperationPurpose, message string) {
	recordNodePreparationOutcome(purpose, "retrying")
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
	if purpose == platformv1alpha1.NodeOperationPurposeScaleDown {
		cluster.Status.Autoscaling.LastScalingDecision = autoscalingDecisionWithContext(cluster, cluster.Status.Autoscaling, fmt.Sprintf("NoScaleDown: %s", message))
	}
}

func progressingReasonForNodePreparation(purpose platformv1alpha1.NodeOperationPurpose) string {
	if purpose == platformv1alpha1.NodeOperationPurposeHibernation {
		return "PreparingNodeForHibernation"
	}
	if purpose == platformv1alpha1.NodeOperationPurposeScaleDown {
		return "PreparingNodeForScaleDown"
	}
	return "PreparingNodeForRestart"
}

func nodePreparationTimeout(cluster *platformv1alpha1.NiFiCluster) time.Duration {
	if cluster.Spec.Hibernation.OffloadTimeout.Duration > 0 {
		return cluster.Spec.Hibernation.OffloadTimeout.Duration
	}
	return defaultNodePreparationTimeout
}

func autoscalingScaleDownNodePreparationRetryReason(operation platformv1alpha1.NodeOperationStatus) string {
	switch operation.Stage {
	case platformv1alpha1.NodeOperationStageDisconnecting:
		return autoscalingBlockedReasonScaleDownDisconnectRetrying
	case platformv1alpha1.NodeOperationStageOffloading:
		return autoscalingBlockedReasonScaleDownOffloadRetrying
	default:
		return autoscalingBlockedReasonScaleDownNodePreparationRetrying
	}
}

func autoscalingScaleDownNodePreparationTimeoutReason(operation platformv1alpha1.NodeOperationStatus) string {
	switch operation.Stage {
	case platformv1alpha1.NodeOperationStageDisconnecting:
		return autoscalingBlockedReasonScaleDownDisconnectTimedOut
	case platformv1alpha1.NodeOperationStageOffloading:
		return autoscalingBlockedReasonScaleDownOffloadTimedOut
	default:
		return autoscalingBlockedReasonScaleDownNodePreparationTimedOut
	}
}

func autoscalingScaleDownNodePreparationGuidance(operation platformv1alpha1.NodeOperationStatus, message string, retrying bool) string {
	stageMessage := "NiFi node preparation is retrying"
	operatorAction := "check controller logs and NiFi API reachability"
	switch operation.Stage {
	case platformv1alpha1.NodeOperationStageDisconnecting:
		stageMessage = "disconnect is not progressing cleanly"
		operatorAction = fmt.Sprintf("verify the target node %s can disconnect cleanly and is not stuck in CONNECTING or DISCONNECTING", emptyIfUnset(operation.NodeID, "for the selected pod"))
	case platformv1alpha1.NodeOperationStageOffloading:
		stageMessage = "offload is not progressing cleanly"
		operatorAction = fmt.Sprintf("verify the target node %s can offload or clear any stuck queued work before autoscaling resumes", emptyIfUnset(operation.NodeID, "for the selected pod"))
	}
	if retrying {
		return fmt.Sprintf("%s. Autoscaling scale-down is blocked because %s. The controller will retry from the same selected scale-down candidate pod %s; operator check: %s.", message, stageMessage, emptyIfUnset(operation.PodName, "once it is available again"), operatorAction)
	}
	return fmt.Sprintf("%s. Autoscaling scale-down is blocked because %s exceeded the configured preparation timeout. Operator action: %s.", message, stageMessage, operatorAction)
}
