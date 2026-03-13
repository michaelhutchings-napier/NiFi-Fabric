package controller

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

func TestObserveStatusTransitionRecordsRolloutMetrics(t *testing.T) {
	resetObservabilityMetrics()

	now := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	original := managedCluster()
	original.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:   platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt: &now,
	}
	original.Status.LastOperation = runningOperation("Rollout", "Config drift rollout is in progress")

	updated := original.DeepCopy()
	updated.Status.Rollout = platformv1alpha1.RolloutStatus{}
	updated.Status.LastOperation = succeededOperation("Rollout", "Config drift rollout completed")

	reconciler := &NiFiClusterReconciler{}
	reconciler.observeStatusTransition(original, updated)

	if got := testutil.ToFloat64(rolloutsTotal.WithLabelValues(string(platformv1alpha1.RolloutTriggerConfigDrift), "completed")); got != 1 {
		t.Fatalf("expected one completed config-drift rollout metric, got %v", got)
	}
	if got := testutil.CollectAndCount(rolloutDurationSeconds); got != 1 {
		t.Fatalf("expected rollout duration histogram sample, got %d", got)
	}
}

func TestObserveStatusTransitionRecordsTLSObservationMetrics(t *testing.T) {
	resetObservabilityMetrics()

	startedAt := metav1.NewTime(time.Now().Add(-45 * time.Second))
	original := managedCluster()
	original.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "TLSAutoreloadObserving",
		Message:            "Observing TLS autoreload",
		LastTransitionTime: metav1.Now(),
	})
	original.Status.TLS = platformv1alpha1.TLSStatus{
		ObservationStartedAt: &startedAt,
	}
	original.Status.LastOperation = runningOperation("TLSObservation", "Observing TLS drift")

	updated := original.DeepCopy()
	updated.Status.TLS = platformv1alpha1.TLSStatus{}
	updated.Status.LastOperation = succeededOperation("TLSObservation", "TLS drift resolved without restart for nifi-tls")

	reconciler := &NiFiClusterReconciler{}
	reconciler.observeStatusTransition(original, updated)

	if got := testutil.ToFloat64(tlsActionsTotal.WithLabelValues("observe_only", "succeeded")); got != 1 {
		t.Fatalf("expected one successful TLS observation metric, got %v", got)
	}
	if got := testutil.CollectAndCount(tlsObservationDurationSeconds); got != 1 {
		t.Fatalf("expected TLS observation duration histogram sample, got %d", got)
	}
}

func TestObserveStatusTransitionRecordsHibernationLifecycleMetrics(t *testing.T) {
	resetObservabilityMetrics()

	reconciler := &NiFiClusterReconciler{}
	original := managedCluster()
	original.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})

	start := original.DeepCopy()
	start.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	start.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Hibernating",
		Message:            "Managed hibernation is in progress",
		LastTransitionTime: metav1.Now(),
	})

	reconciler.observeStatusTransition(original, start)

	if got := testutil.ToFloat64(hibernationOperationsTotal.WithLabelValues("hibernate", "started")); got != 1 {
		t.Fatalf("expected one hibernation-started metric, got %v", got)
	}

	completed := start.DeepCopy()
	completed.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionTrue,
		Reason:             "Hibernated",
		Message:            "Cluster is hibernated",
		LastTransitionTime: metav1.Now(),
	})

	reconciler.observeStatusTransition(start, completed)

	if got := testutil.ToFloat64(hibernationOperationsTotal.WithLabelValues("hibernate", "completed")); got != 1 {
		t.Fatalf("expected one hibernation-completed metric, got %v", got)
	}
}

func TestNodePreparationMetricsRecordRetryAndTimeout(t *testing.T) {
	resetObservabilityMetrics()

	reconciler := &NiFiClusterReconciler{}
	cluster := managedCluster()

	reconciler.markNodePreparationBlocked(cluster, platformv1alpha1.NodeOperationPurposeRestart, "retrying after lifecycle error")
	reconciler.markNodePreparationTimedOut(cluster, platformv1alpha1.NodeOperationPurposeHibernation, "timed out waiting for offload")

	if got := testutil.ToFloat64(nodePreparationOutcomesTotal.WithLabelValues(string(platformv1alpha1.NodeOperationPurposeRestart), "retrying")); got != 1 {
		t.Fatalf("expected one retrying node-preparation metric, got %v", got)
	}
	if got := testutil.ToFloat64(nodePreparationOutcomesTotal.WithLabelValues(string(platformv1alpha1.NodeOperationPurposeHibernation), "timed_out")); got != 1 {
		t.Fatalf("expected one timed-out node-preparation metric, got %v", got)
	}
}

func TestObserveStatusTransitionRecordsAutoscalingMetrics(t *testing.T) {
	resetObservabilityMetrics()

	original := managedCluster()
	original.Status.Autoscaling = platformv1alpha1.AutoscalingStatus{
		Reason: autoscalingReasonProgressing,
	}

	recommended := int32(4)
	updated := original.DeepCopy()
	updated.Status.Autoscaling = platformv1alpha1.AutoscalingStatus{
		RecommendedReplicas: &recommended,
		Reason:              autoscalingReasonBelowMinReplicas,
	}

	reconciler := &NiFiClusterReconciler{}
	reconciler.observeStatusTransition(original, updated)

	if got := testutil.ToFloat64(autoscalingRecommendationsTotal.WithLabelValues(autoscalingReasonBelowMinReplicas, "increase")); got != 1 {
		t.Fatalf("expected one advisory autoscaling recommendation metric, got %v", got)
	}
	if got := testutil.ToFloat64(autoscalingRecommendedReplicas.WithLabelValues(updated.Namespace, updated.Name)); got != 4 {
		t.Fatalf("expected recommended replicas gauge to be 4, got %v", got)
	}
}

func TestObserveStatusTransitionRecordsAutoscalingExecutionTransitionMetrics(t *testing.T) {
	resetObservabilityMetrics()

	original := managedCluster()
	original.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:   platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare,
		State:   platformv1alpha1.AutoscalingExecutionStateRunning,
		Message: "Preparing pod nifi-2 for safe autoscaling scale-down",
	}

	updated := original.DeepCopy()
	updated.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:         platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare,
		State:         platformv1alpha1.AutoscalingExecutionStateBlocked,
		BlockedReason: "NodePreparationTimedOut",
		Message:       "timed out waiting for NiFi node node-2 to reach OFFLOADED before proceeding",
	}

	reconciler := &NiFiClusterReconciler{}
	reconciler.observeStatusTransition(original, updated)

	if got := testutil.ToFloat64(autoscalingExecutionTransitionsTotal.WithLabelValues(
		string(platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare),
		string(platformv1alpha1.AutoscalingExecutionStateBlocked),
		"NodePreparationTimedOut",
	)); got != 1 {
		t.Fatalf("expected one autoscaling execution transition metric, got %v", got)
	}
}

func TestObserveStatusTransitionIgnoresAutoscalingMessageOnlyChanges(t *testing.T) {
	resetObservabilityMetrics()

	recommended := int32(3)
	original := managedCluster()
	original.Status.Autoscaling = platformv1alpha1.AutoscalingStatus{
		RecommendedReplicas: &recommended,
		Reason:              autoscalingReasonNoActionableInput,
		Signals: []platformv1alpha1.AutoscalingSignalStatus{{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: true,
			Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=2/10",
		}},
	}

	updated := original.DeepCopy()
	updated.Status.Autoscaling.Signals[0].Message = "queuedFlowFiles=1 queuedBytes=0 activeTimerDrivenThreads=2/10"

	reconciler := &NiFiClusterReconciler{}
	reconciler.observeStatusTransition(original, updated)

	if got := testutil.ToFloat64(autoscalingRecommendationsTotal.WithLabelValues(autoscalingReasonNoActionableInput, "hold")); got != 0 {
		t.Fatalf("expected message-only autoscaling changes not to increment recommendation metrics, got %v", got)
	}
}

func TestRecordAutoscalingScaleActionRecordsMetric(t *testing.T) {
	resetObservabilityMetrics()

	recordAutoscalingScaleAction("scaled_up")
	recordAutoscalingScaleAction("scaled_down")

	if got := testutil.ToFloat64(autoscalingScaleActionsTotal.WithLabelValues("scaled_up")); got != 1 {
		t.Fatalf("expected one autoscaling scale action metric, got %v", got)
	}
	if got := testutil.ToFloat64(autoscalingScaleActionsTotal.WithLabelValues("scaled_down")); got != 1 {
		t.Fatalf("expected one autoscaling scale-down action metric, got %v", got)
	}
}

func resetObservabilityMetrics() {
	lifecycleTransitionsTotal.Reset()
	rolloutsTotal.Reset()
	rolloutDurationSeconds.Reset()
	tlsActionsTotal.Reset()
	tlsObservationDurationSeconds.Reset()
	hibernationOperationsTotal.Reset()
	hibernationDurationSeconds.Reset()
	nodePreparationOutcomesTotal.Reset()
	autoscalingRecommendationsTotal.Reset()
	autoscalingScaleActionsTotal.Reset()
	autoscalingExecutionTransitionsTotal.Reset()
	autoscalingRecommendedReplicas.Reset()
	autoscalingSignalSamples.Reset()
}
