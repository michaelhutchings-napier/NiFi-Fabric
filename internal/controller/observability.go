package controller

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

var lifecycleTransitionsTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "nifi_platform_lifecycle_transitions_total",
		Help: "Count of notable rollout, TLS, and hibernation lifecycle transitions observed by the controller.",
	},
	[]string{"category", "event"},
)

func init() {
	ctrlmetrics.Registry.MustRegister(lifecycleTransitionsTotal)
}

type lifecycleSignal struct {
	category string
	event    string
	level    string
	reason   string
	message  string
}

func (r *NiFiClusterReconciler) observeStatusTransition(original, updated *platformv1alpha1.NiFiCluster) {
	for _, signal := range collectLifecycleSignals(original, updated) {
		lifecycleTransitionsTotal.WithLabelValues(signal.category, signal.event).Inc()
		if r.Recorder != nil {
			r.Recorder.Event(updated, signal.level, signal.reason, signal.message)
		}
	}
}

func collectLifecycleSignals(original, updated *platformv1alpha1.NiFiCluster) []lifecycleSignal {
	signals := make([]lifecycleSignal, 0, 8)

	if signal, ok := rolloutSignalFromProgressing(original, updated); ok {
		signals = append(signals, signal)
	}
	if signal, ok := tlsSignalFromStatus(original, updated); ok {
		signals = append(signals, signal)
	}
	if signal, ok := hibernationSignalFromStatus(original, updated); ok {
		signals = append(signals, signal)
	}
	if signal, ok := degradationSignal(original, updated); ok {
		signals = append(signals, signal)
	}
	if signal, ok := completionSignal(original, updated); ok {
		signals = append(signals, signal)
	}

	return signals
}

func rolloutSignalFromProgressing(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	oldCondition := original.GetCondition(platformv1alpha1.ConditionProgressing)
	newCondition := updated.GetCondition(platformv1alpha1.ConditionProgressing)
	if !conditionChangedSignificantly(oldCondition, newCondition) || newCondition == nil {
		return lifecycleSignal{}, false
	}

	switch newCondition.Reason {
	case "RevisionDriftDetected":
		return lifecycleSignal{
			category: "rollout",
			event:    "revision_drift_detected",
			level:    corev1.EventTypeNormal,
			reason:   "RevisionDriftDetected",
			message:  newCondition.Message,
		}, true
	case "ConfigDriftDetected":
		return lifecycleSignal{
			category: "rollout",
			event:    "config_drift_detected",
			level:    corev1.EventTypeNormal,
			reason:   "ConfigDriftDetected",
			message:  newCondition.Message,
		}, true
	case "PodDeleted":
		if updated.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
			return lifecycleSignal{
				category: "tls",
				event:    "rollout_pod_deleted",
				level:    corev1.EventTypeNormal,
				reason:   "TLSRolloutPodDeleted",
				message:  newCondition.Message,
			}, true
		}
		return lifecycleSignal{
			category: "rollout",
			event:    "pod_deleted",
			level:    corev1.EventTypeNormal,
			reason:   "RolloutPodDeleted",
			message:  newCondition.Message,
		}, true
	case "TLSAutoreloadObserving":
		return lifecycleSignal{
			category: "tls",
			event:    "observing_autoreload",
			level:    corev1.EventTypeNormal,
			reason:   "TLSAutoreloadObserving",
			message:  newCondition.Message,
		}, true
	case "TLSRolloutInProgress":
		return lifecycleSignal{
			category: "tls",
			event:    "rollout_required",
			level:    corev1.EventTypeNormal,
			reason:   "TLSRolloutRequired",
			message:  newCondition.Message,
		}, true
	case "ScalingDown":
		if updated.Spec.DesiredState != platformv1alpha1.DesiredStateHibernated {
			return lifecycleSignal{}, false
		}
		return lifecycleSignal{
			category: "hibernation",
			event:    "scaling_down_step",
			level:    corev1.EventTypeNormal,
			reason:   "HibernationScalingDown",
			message:  newCondition.Message,
		}, true
	case "ScalingUp", "WaitingForPodsReady", "WaitingForClusterHealth":
		newHibernated := updated.GetCondition(platformv1alpha1.ConditionHibernated)
		if newHibernated != nil && newHibernated.Reason == "Restoring" {
			return lifecycleSignal{
				category: "hibernation",
				event:    "restore_in_progress",
				level:    corev1.EventTypeNormal,
				reason:   "RestoreInProgress",
				message:  newCondition.Message,
			}, true
		}
	case "PreparingNodeForRestart":
		return lifecycleSignal{
			category: "rollout",
			event:    "preparing_node",
			level:    corev1.EventTypeNormal,
			reason:   "PreparingNodeForRestart",
			message:  newCondition.Message,
		}, true
	case "PreparingNodeForHibernation":
		return lifecycleSignal{
			category: "hibernation",
			event:    "preparing_node",
			level:    corev1.EventTypeNormal,
			reason:   "PreparingNodeForHibernation",
			message:  newCondition.Message,
		}, true
	}

	return lifecycleSignal{}, false
}

func tlsSignalFromStatus(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	if sameLastOperationMeaning(original.Status.LastOperation, updated.Status.LastOperation) {
		return lifecycleSignal{}, false
	}

	switch {
	case updated.Status.LastOperation.Type == "TLSObservation" && updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded:
		return lifecycleSignal{
			category: "tls",
			event:    "resolved_without_restart",
			level:    corev1.EventTypeNormal,
			reason:   "TLSDriftResolvedWithoutRestart",
			message:  updated.Status.LastOperation.Message,
		}, true
	case updated.Status.LastOperation.Type == "TLSObservation" && strings.Contains(updated.Status.LastOperation.Message, "health degraded"):
		return lifecycleSignal{
			category: "tls",
			event:    "observation_degraded",
			level:    corev1.EventTypeWarning,
			reason:   "TLSObservationHealthDegraded",
			message:  updated.Status.LastOperation.Message,
		}, true
	case updated.Status.LastOperation.Type == "Rollout" &&
		updated.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift &&
		updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded:
		return lifecycleSignal{
			category: "tls",
			event:    "rollout_completed",
			level:    corev1.EventTypeNormal,
			reason:   "TLSRolloutCompleted",
			message:  updated.Status.LastOperation.Message,
		}, true
	}

	return lifecycleSignal{}, false
}

func hibernationSignalFromStatus(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	oldCondition := original.GetCondition(platformv1alpha1.ConditionHibernated)
	newCondition := updated.GetCondition(platformv1alpha1.ConditionHibernated)
	if !conditionChangedSignificantly(oldCondition, newCondition) || newCondition == nil {
		return lifecycleSignal{}, false
	}

	switch {
	case newCondition.Status == metav1.ConditionTrue && newCondition.Reason == "Hibernated":
		return lifecycleSignal{
			category: "hibernation",
			event:    "completed",
			level:    corev1.EventTypeNormal,
			reason:   "Hibernated",
			message:  newCondition.Message,
		}, true
	case newCondition.Status == metav1.ConditionFalse && newCondition.Reason == "Restoring":
		return lifecycleSignal{
			category: "hibernation",
			event:    "restore_started",
			level:    corev1.EventTypeNormal,
			reason:   "RestoreStarted",
			message:  newCondition.Message,
		}, true
	}

	return lifecycleSignal{}, false
}

func degradationSignal(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	oldCondition := original.GetCondition(platformv1alpha1.ConditionDegraded)
	newCondition := updated.GetCondition(platformv1alpha1.ConditionDegraded)
	if !conditionChangedSignificantly(oldCondition, newCondition) || newCondition == nil {
		return lifecycleSignal{}, false
	}

	switch newCondition.Reason {
	case "NodePreparationTimedOut":
		category := "rollout"
		if updated.Status.NodeOperation.Purpose == platformv1alpha1.NodeOperationPurposeHibernation || updated.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated {
			category = "hibernation"
		}
		return lifecycleSignal{
			category: category,
			event:    "node_preparation_timed_out",
			level:    corev1.EventTypeWarning,
			reason:   "NodePreparationTimedOut",
			message:  newCondition.Message,
		}, true
	case "HibernationFailed":
		return lifecycleSignal{
			category: "hibernation",
			event:    "failed",
			level:    corev1.EventTypeWarning,
			reason:   "HibernationFailed",
			message:  newCondition.Message,
		}, true
	case "RolloutFailed":
		return lifecycleSignal{
			category: "rollout",
			event:    "failed",
			level:    corev1.EventTypeWarning,
			reason:   "RolloutFailed",
			message:  newCondition.Message,
		}, true
	}

	return lifecycleSignal{}, false
}

func completionSignal(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	if sameLastOperationMeaning(original.Status.LastOperation, updated.Status.LastOperation) {
		return lifecycleSignal{}, false
	}

	switch {
	case updated.Status.LastOperation.Type == "Rollout" && updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded:
		return lifecycleSignal{
			category: "rollout",
			event:    "completed",
			level:    corev1.EventTypeNormal,
			reason:   "RolloutCompleted",
			message:  updated.Status.LastOperation.Message,
		}, true
	case updated.Status.LastOperation.Type == "Hibernation" &&
		updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded &&
		updated.Spec.DesiredState == platformv1alpha1.DesiredStateRunning:
		return lifecycleSignal{
			category: "hibernation",
			event:    "restore_completed",
			level:    corev1.EventTypeNormal,
			reason:   "RestoreCompleted",
			message:  updated.Status.LastOperation.Message,
		}, true
	}

	return lifecycleSignal{}, false
}

func conditionChangedSignificantly(oldCondition, newCondition *metav1.Condition) bool {
	if oldCondition == nil && newCondition == nil {
		return false
	}
	if oldCondition == nil || newCondition == nil {
		return true
	}
	return oldCondition.Status != newCondition.Status ||
		oldCondition.Reason != newCondition.Reason ||
		oldCondition.Message != newCondition.Message
}

func sameLastOperationMeaning(left, right platformv1alpha1.LastOperation) bool {
	return left.Type == right.Type &&
		left.Phase == right.Phase &&
		left.Message == right.Message
}
