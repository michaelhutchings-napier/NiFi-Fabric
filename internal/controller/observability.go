package controller

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

var (
	lifecycleTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_lifecycle_transitions_total",
			Help: "Count of notable rollout, TLS, and hibernation lifecycle transitions observed by the controller.",
		},
		[]string{"category", "event"},
	)
	rolloutsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_rollouts_total",
			Help: "Count of managed rollout transitions by trigger and result.",
		},
		[]string{"trigger", "result"},
	)
	rolloutDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nifi_platform_rollout_duration_seconds",
			Help:    "Duration of managed rollouts by trigger and result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"trigger", "result"},
	)
	tlsActionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_tls_actions_total",
			Help: "Count of TLS observation and restart-required decisions by result.",
		},
		[]string{"action", "result"},
	)
	tlsObservationDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nifi_platform_tls_observation_duration_seconds",
			Help:    "Duration of TLS autoreload observation windows by result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)
	hibernationOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_hibernation_operations_total",
			Help: "Count of hibernation and restore lifecycle transitions by result.",
		},
		[]string{"operation", "result"},
	)
	hibernationDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nifi_platform_hibernation_duration_seconds",
			Help:    "Duration of hibernation and restore operations by result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "result"},
	)
	nodePreparationOutcomesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_node_preparation_outcomes_total",
			Help: "Count of node-preparation retry and timeout outcomes observed by the controller.",
		},
		[]string{"purpose", "result"},
	)
	autoscalingRecommendationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nifi_platform_autoscaling_recommendations_total",
			Help: "Count of advisory autoscaling recommendation transitions by reason and outcome.",
		},
		[]string{"reason", "outcome"},
	)
	autoscalingRecommendedReplicas = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nifi_platform_autoscaling_recommended_replicas",
			Help: "Latest advisory autoscaling replica recommendation for each NiFiCluster. Zero means no active recommendation.",
		},
		[]string{"namespace", "name"},
	)
	autoscalingSignalSamples = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "nifi_platform_autoscaling_signal_sample",
			Help: "Latest advisory autoscaling signal samples observed for each NiFiCluster.",
		},
		[]string{"namespace", "name", "signal", "sample"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		lifecycleTransitionsTotal,
		rolloutsTotal,
		rolloutDurationSeconds,
		tlsActionsTotal,
		tlsObservationDurationSeconds,
		hibernationOperationsTotal,
		hibernationDurationSeconds,
		nodePreparationOutcomesTotal,
		autoscalingRecommendationsTotal,
		autoscalingRecommendedReplicas,
		autoscalingSignalSamples,
	)
	warmObservabilityMetrics()
}

func warmObservabilityMetrics() {
	lifecycleTransitionsTotal.WithLabelValues("rollout", "started")
	rolloutsTotal.WithLabelValues(string(platformv1alpha1.RolloutTriggerStatefulSetRevision), "started")
	rolloutDurationSeconds.WithLabelValues(string(platformv1alpha1.RolloutTriggerStatefulSetRevision), "completed")
	tlsActionsTotal.WithLabelValues("observe_only", "started")
	tlsObservationDurationSeconds.WithLabelValues("succeeded")
	hibernationOperationsTotal.WithLabelValues("hibernate", "started")
	hibernationDurationSeconds.WithLabelValues("hibernate", "completed")
	nodePreparationOutcomesTotal.WithLabelValues(string(platformv1alpha1.NodeOperationPurposeRestart), "retrying")
	autoscalingRecommendationsTotal.WithLabelValues(autoscalingReasonNoActionableInput, "hold")
	autoscalingSignalSamples.WithLabelValues("", "", string(platformv1alpha1.AutoscalingSignalQueuePressure), "flow_files_queued")
}

type lifecycleSignal struct {
	category string
	event    string
	level    string
	reason   string
	message  string
}

type observabilityState struct {
	mu      sync.Mutex
	started map[string]time.Time
}

func (r *NiFiClusterReconciler) observeStatusTransition(original, updated *platformv1alpha1.NiFiCluster) {
	signals := collectLifecycleSignals(original, updated)
	for _, signal := range signals {
		lifecycleTransitionsTotal.WithLabelValues(signal.category, signal.event).Inc()
		if r.Recorder != nil {
			r.Recorder.Event(updated, signal.level, signal.reason, signal.message)
		}
	}
	r.observeOperationMetrics(original, updated)
}

func (r *NiFiClusterReconciler) observeOperationMetrics(original, updated *platformv1alpha1.NiFiCluster) {
	observeRolloutMetrics(original, updated)
	observeTLSMetrics(original, updated)
	r.observeHibernationMetrics(original, updated)
	observeAutoscalingMetrics(original, updated)
}

func collectLifecycleSignals(original, updated *platformv1alpha1.NiFiCluster) []lifecycleSignal {
	signals := make([]lifecycleSignal, 0, 10)

	if signal, ok := rolloutStartSignal(original, updated); ok {
		signals = append(signals, signal)
	}
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
	if signal, ok := autoscalingSignalFromStatus(original, updated); ok {
		signals = append(signals, signal)
	}

	return signals
}

func rolloutStartSignal(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	if updated.Status.Rollout.Trigger == "" {
		return lifecycleSignal{}, false
	}
	if original.Status.Rollout.Trigger == updated.Status.Rollout.Trigger &&
		original.Status.Rollout.TargetRevision == updated.Status.Rollout.TargetRevision &&
		original.Status.Rollout.TargetConfigHash == updated.Status.Rollout.TargetConfigHash &&
		original.Status.Rollout.TargetCertificateHash == updated.Status.Rollout.TargetCertificateHash &&
		original.Status.Rollout.TargetTLSConfigurationHash == updated.Status.Rollout.TargetTLSConfigurationHash {
		return lifecycleSignal{}, false
	}

	message := fmt.Sprintf("Managed rollout started for %s", updated.Status.Rollout.Trigger)
	switch updated.Status.Rollout.Trigger {
	case platformv1alpha1.RolloutTriggerStatefulSetRevision:
		if updated.Status.Rollout.TargetRevision != "" {
			message = fmt.Sprintf("Managed rollout started toward StatefulSet revision %q", updated.Status.Rollout.TargetRevision)
		}
		return lifecycleSignal{
			category: "rollout",
			event:    "started",
			level:    corev1.EventTypeNormal,
			reason:   "RolloutStarted",
			message:  message,
		}, true
	case platformv1alpha1.RolloutTriggerConfigDrift:
		return lifecycleSignal{
			category: "rollout",
			event:    "started",
			level:    corev1.EventTypeNormal,
			reason:   "RolloutStarted",
			message:  message,
		}, true
	case platformv1alpha1.RolloutTriggerTLSDrift:
		return lifecycleSignal{
			category: "tls",
			event:    "rollout_required",
			level:    corev1.EventTypeNormal,
			reason:   "TLSRolloutRequired",
			message:  message,
		}, true
	default:
		return lifecycleSignal{}, false
	}
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
			event:    "observe_only_started",
			level:    corev1.EventTypeNormal,
			reason:   "TLSAutoreloadObserving",
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
	case newCondition.Status == metav1.ConditionFalse && newCondition.Reason == "Hibernating":
		return lifecycleSignal{
			category: "hibernation",
			event:    "started",
			level:    corev1.EventTypeNormal,
			reason:   "HibernationStarted",
			message:  newCondition.Message,
		}, true
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

func autoscalingSignalFromStatus(original, updated *platformv1alpha1.NiFiCluster) (lifecycleSignal, bool) {
	if autoscalingStatusMeaningEqual(original.Status.Autoscaling, updated.Status.Autoscaling) {
		return lifecycleSignal{}, false
	}
	if updated.Status.Autoscaling.Reason == autoscalingReasonDisabled {
		return lifecycleSignal{}, false
	}

	reason := "AutoscalingRecommendationUpdated"
	if updated.Status.Autoscaling.RecommendedReplicas == nil {
		reason = "AutoscalingRecommendationBlocked"
	}

	return lifecycleSignal{
		category: "autoscaling",
		event:    autoscalingStatusOutcome(updated.Status.Autoscaling),
		level:    corev1.EventTypeNormal,
		reason:   reason,
		message:  autoscalingStatusMessage(updated.Status.Autoscaling),
	}, true
}

func observeRolloutMetrics(original, updated *platformv1alpha1.NiFiCluster) {
	trigger := updated.Status.Rollout.Trigger
	if trigger != "" && original.Status.Rollout.Trigger != trigger {
		rolloutsTotal.WithLabelValues(string(trigger), "started").Inc()
	}

	if updated.Status.LastOperation.Type != "Rollout" || sameLastOperationMeaning(original.Status.LastOperation, updated.Status.LastOperation) {
		return
	}

	switch updated.Status.LastOperation.Phase {
	case platformv1alpha1.OperationPhaseSucceeded:
		completedTrigger := original.Status.Rollout.Trigger
		if completedTrigger == "" {
			completedTrigger = updated.Status.Rollout.Trigger
		}
		if completedTrigger == "" {
			return
		}
		rolloutsTotal.WithLabelValues(string(completedTrigger), "completed").Inc()
		observeDurationFromMetav1(rolloutDurationSeconds.WithLabelValues(string(completedTrigger), "completed"), original.Status.Rollout.StartedAt)
	case platformv1alpha1.OperationPhaseFailed:
		failedTrigger := original.Status.Rollout.Trigger
		if failedTrigger == "" {
			failedTrigger = updated.Status.Rollout.Trigger
		}
		if failedTrigger == "" {
			return
		}
		rolloutsTotal.WithLabelValues(string(failedTrigger), "failed").Inc()
		observeDurationFromMetav1(rolloutDurationSeconds.WithLabelValues(string(failedTrigger), "failed"), original.Status.Rollout.StartedAt)
	}
}

func observeTLSMetrics(original, updated *platformv1alpha1.NiFiCluster) {
	if progressingConditionReasonChanged(original, updated, "TLSAutoreloadObserving") {
		tlsActionsTotal.WithLabelValues("observe_only", "started").Inc()
	}

	if updated.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift &&
		original.Status.Rollout.Trigger != platformv1alpha1.RolloutTriggerTLSDrift {
		tlsActionsTotal.WithLabelValues("restart_required", "started").Inc()
	}

	if sameLastOperationMeaning(original.Status.LastOperation, updated.Status.LastOperation) {
		return
	}

	switch {
	case updated.Status.LastOperation.Type == "TLSObservation" && updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded:
		tlsActionsTotal.WithLabelValues("observe_only", "succeeded").Inc()
		observeDurationFromMetav1(tlsObservationDurationSeconds.WithLabelValues("succeeded"), original.Status.TLS.ObservationStartedAt)
	case updated.Status.LastOperation.Type == "TLSObservation" && strings.Contains(updated.Status.LastOperation.Message, "health degraded"):
		tlsActionsTotal.WithLabelValues("observe_only", "degraded").Inc()
		observeDurationFromMetav1(tlsObservationDurationSeconds.WithLabelValues("degraded"), original.Status.TLS.ObservationStartedAt)
	case updated.Status.LastOperation.Type == "Rollout" &&
		updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded &&
		original.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift:
		tlsActionsTotal.WithLabelValues("restart_required", "completed").Inc()
	case updated.Status.LastOperation.Type == "Rollout" &&
		updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseFailed &&
		original.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift:
		tlsActionsTotal.WithLabelValues("restart_required", "failed").Inc()
	}
}

func (r *NiFiClusterReconciler) observeHibernationMetrics(original, updated *platformv1alpha1.NiFiCluster) {
	state := r.observabilityState()

	if hibernatedConditionReasonChanged(original, updated, "Hibernating") {
		state.start(observabilityKey(updated, "hibernation"))
		hibernationOperationsTotal.WithLabelValues("hibernate", "started").Inc()
	}
	if hibernatedConditionReasonChanged(original, updated, "Restoring") {
		state.start(observabilityKey(updated, "restore"))
		hibernationOperationsTotal.WithLabelValues("restore", "started").Inc()
	}

	if conditionHasReason(updated.GetCondition(platformv1alpha1.ConditionHibernated), metav1.ConditionTrue, "Hibernated") &&
		!conditionHasReason(original.GetCondition(platformv1alpha1.ConditionHibernated), metav1.ConditionTrue, "Hibernated") {
		hibernationOperationsTotal.WithLabelValues("hibernate", "completed").Inc()
		if duration, ok := state.finish(observabilityKey(updated, "hibernation")); ok {
			hibernationDurationSeconds.WithLabelValues("hibernate", "completed").Observe(duration.Seconds())
		}
	}

	if updated.Status.LastOperation.Type == "Hibernation" &&
		updated.Status.LastOperation.Phase == platformv1alpha1.OperationPhaseSucceeded &&
		updated.Spec.DesiredState == platformv1alpha1.DesiredStateRunning &&
		!sameLastOperationMeaning(original.Status.LastOperation, updated.Status.LastOperation) {
		hibernationOperationsTotal.WithLabelValues("restore", "completed").Inc()
		if duration, ok := state.finish(observabilityKey(updated, "restore")); ok {
			hibernationDurationSeconds.WithLabelValues("restore", "completed").Observe(duration.Seconds())
		}
	}

	if degradedConditionReasonChanged(original, updated, "HibernationFailed") {
		hibernationOperationsTotal.WithLabelValues("hibernate", "failed").Inc()
		if duration, ok := state.finish(observabilityKey(updated, "hibernation")); ok {
			hibernationDurationSeconds.WithLabelValues("hibernate", "failed").Observe(duration.Seconds())
		}
	}
}

func observeAutoscalingMetrics(original, updated *platformv1alpha1.NiFiCluster) {
	if autoscalingStatusMeaningEqual(original.Status.Autoscaling, updated.Status.Autoscaling) {
		return
	}

	autoscalingRecommendationsTotal.WithLabelValues(
		updated.Status.Autoscaling.Reason,
		autoscalingStatusOutcome(updated.Status.Autoscaling),
	).Inc()
	autoscalingRecommendedReplicas.WithLabelValues(updated.Namespace, updated.Name).Set(
		float64(derefOptionalInt32(updated.Status.Autoscaling.RecommendedReplicas)),
	)
}

func recordAutoscalingSignalSamples(cluster *platformv1alpha1.NiFiCluster, collection autoscalingSignalCollection) {
	namespace := cluster.Namespace
	name := cluster.Name

	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalQueuePressure), "flow_files_queued").Set(float64(collection.QueuePressure.FlowFilesQueued))
	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalQueuePressure), "bytes_queued").Set(float64(collection.QueuePressure.BytesQueued))
	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalQueuePressure), "active_timer_driven_threads").Set(float64(collection.QueuePressure.ActiveTimerDrivenThreads))
	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalQueuePressure), "max_timer_driven_threads").Set(float64(collection.QueuePressure.MaxTimerDrivenThreads))
	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalCPU), "load_average").Set(collection.CPU.LoadAverage)
	autoscalingSignalSamples.WithLabelValues(namespace, name, string(platformv1alpha1.AutoscalingSignalCPU), "available_processors").Set(float64(collection.CPU.AvailableProcessors))
}

func (r *NiFiClusterReconciler) observabilityState() *observabilityState {
	if r.observability == nil {
		r.observability = &observabilityState{started: map[string]time.Time{}}
	}
	return r.observability
}

func (s *observabilityState) start(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started == nil {
		s.started = map[string]time.Time{}
	}
	if _, exists := s.started[key]; !exists {
		s.started[key] = time.Now().UTC()
	}
}

func (s *observabilityState) finish(key string) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	startedAt, exists := s.started[key]
	if !exists {
		return 0, false
	}
	delete(s.started, key)
	return time.Since(startedAt), true
}

func observabilityKey(cluster *platformv1alpha1.NiFiCluster, operation string) string {
	return fmt.Sprintf("%s/%s/%s", cluster.Namespace, cluster.Name, operation)
}

func recordNodePreparationOutcome(purpose platformv1alpha1.NodeOperationPurpose, result string) {
	nodePreparationOutcomesTotal.WithLabelValues(string(purpose), result).Inc()
}

func observeDurationFromMetav1(observer prometheus.Observer, startedAt *metav1.Time) {
	if startedAt == nil || observer == nil {
		return
	}
	duration := time.Since(startedAt.Time)
	if duration < 0 {
		return
	}
	observer.Observe(duration.Seconds())
}

func progressingConditionReasonChanged(original, updated *platformv1alpha1.NiFiCluster, reason string) bool {
	return conditionChangedToReason(
		original.GetCondition(platformv1alpha1.ConditionProgressing),
		updated.GetCondition(platformv1alpha1.ConditionProgressing),
		metav1.ConditionTrue,
		reason,
	)
}

func hibernatedConditionReasonChanged(original, updated *platformv1alpha1.NiFiCluster, reason string) bool {
	return conditionChangedToReason(
		original.GetCondition(platformv1alpha1.ConditionHibernated),
		updated.GetCondition(platformv1alpha1.ConditionHibernated),
		metav1.ConditionFalse,
		reason,
	)
}

func degradedConditionReasonChanged(original, updated *platformv1alpha1.NiFiCluster, reason string) bool {
	return conditionChangedToReason(
		original.GetCondition(platformv1alpha1.ConditionDegraded),
		updated.GetCondition(platformv1alpha1.ConditionDegraded),
		metav1.ConditionTrue,
		reason,
	)
}

func conditionChangedToReason(oldCondition, newCondition *metav1.Condition, status metav1.ConditionStatus, reason string) bool {
	return conditionHasReason(newCondition, status, reason) && !conditionHasReason(oldCondition, status, reason)
}

func conditionHasReason(condition *metav1.Condition, status metav1.ConditionStatus, reason string) bool {
	return condition != nil && condition.Status == status && condition.Reason == reason
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
