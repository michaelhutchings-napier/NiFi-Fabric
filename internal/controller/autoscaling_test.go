package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
	"github.com/michaelhutchings-napier/NiFi-Fabric/internal/nifi"
)

func TestBuildAutoscalingStatusAdvisoryHoldsCurrentReplicas(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{
		SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: true,
			Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=2/10",
		}},
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			FlowFilesQueued:          0,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 2,
			MaxTimerDrivenThreads:    10,
		},
	})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 3 {
		t.Fatalf("expected advisory recommendation to hold at 3 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonNoActionableInput {
		t.Fatalf("expected no-actionable-signals reason, got %q", status.Reason)
	}
	if len(status.Signals) != 1 || status.Signals[0].Type != platformv1alpha1.AutoscalingSignalQueuePressure {
		t.Fatalf("expected default queue-pressure signal, got %#v", status.Signals)
	}
	if !status.Signals[0].Available {
		t.Fatalf("expected queue-pressure signal to be available")
	}
}

func TestBuildAutoscalingStatusClampsToConfiguredMinimum(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 4,
		MaxReplicas: 6,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
			platformv1alpha1.AutoscalingSignalCPU,
		},
	}
	cluster.Status.Replicas.Desired = 2
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{
		SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{
			{Type: platformv1alpha1.AutoscalingSignalQueuePressure, Available: true, Message: "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10"},
			{Type: platformv1alpha1.AutoscalingSignalCPU, Available: true, Message: "loadAverage=0.25 availableProcessors=2"},
		},
	})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 4 {
		t.Fatalf("expected advisory recommendation to clamp to min replicas 4, got %d", got)
	}
	if status.Reason != autoscalingReasonBelowMinReplicas {
		t.Fatalf("expected below-min-replicas reason, got %q", status.Reason)
	}
	if len(status.Signals) != 2 {
		t.Fatalf("expected two configured advisory signals, got %#v", status.Signals)
	}
}

func TestBuildAutoscalingStatusScalesUpForActionableQueuePressure(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 2,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 2
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{
		SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: true,
			Message:   "queuedFlowFiles=64 queuedBytes=1048576 activeTimerDrivenThreads=10/10 backlog is actionable",
		}},
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			FlowFilesQueued:          64,
			BytesQueued:              1048576,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 10,
			MaxTimerDrivenThreads:    10,
			Actionable:               true,
		},
	})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 3 {
		t.Fatalf("expected queue pressure to recommend 3 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonQueuePressure {
		t.Fatalf("expected queue-pressure reason, got %q", status.Reason)
	}
}

func TestBuildAutoscalingStatusUsesExternalScaleUpIntent(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeAdvisory,
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			RequestedReplicas: 6,
		},
		MinReplicas: 2,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 2
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 4 {
		t.Fatalf("expected external scale-up intent to be bounded to 4 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonExternalScaleUp {
		t.Fatalf("expected external scale-up reason, got %q", status.Reason)
	}
	if !status.External.Observed || !status.External.Actionable {
		t.Fatalf("expected external intent to be observed and actionable, got %#v", status.External)
	}
	if got := derefOptionalInt32(status.External.RequestedReplicas); got != 6 {
		t.Fatalf("expected status to retain the raw external requested replicas 6, got %d", got)
	}
	if !strings.Contains(status.External.Message, "bounded the scale-up intent to 4") {
		t.Fatalf("expected bounded external scale-up message, got %q", status.External.Message)
	}
}

func TestBuildAutoscalingStatusUsesExternalScaleDownIntentWhenEnabled(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 1,
		},
		MinReplicas: 2,
		MaxReplicas: 5,
	}
	cluster.Status.Replicas.Desired = 4
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 2 {
		t.Fatalf("expected external scale-down intent to be bounded to min replicas 2, got %d", got)
	}
	if status.Reason != autoscalingReasonExternalScaleDown {
		t.Fatalf("expected external scale-down reason, got %q", status.Reason)
	}
	if !status.External.Observed || !status.External.Actionable || status.External.ScaleDownIgnored {
		t.Fatalf("expected external scale-down intent to be observed and actionable, got %#v", status.External)
	}
	if got := derefOptionalInt32(status.External.RequestedReplicas); got != 1 {
		t.Fatalf("expected status to retain the raw external requested replicas 1, got %d", got)
	}
	if !strings.Contains(status.External.Message, "bounded the best-effort scale-down intent to 2") {
		t.Fatalf("expected bounded external scale-down message, got %q", status.External.Message)
	}
}

func TestBuildAutoscalingStatusIgnoresExternalScaleDownIntent(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  false,
			RequestedReplicas: 2,
		},
		MinReplicas: 1,
		MaxReplicas: 5,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 3 {
		t.Fatalf("expected ignored external scale-down to hold at 3 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonNoActionableInput {
		t.Fatalf("expected no-actionable reason, got %q", status.Reason)
	}
	if !status.External.Observed || !status.External.ScaleDownIgnored {
		t.Fatalf("expected external scale-down intent to be marked ignored, got %#v", status.External)
	}
	decision := autoscalingNoScaleDecision(cluster, status)
	if !strings.Contains(decision, "external scale-down intent is disabled") {
		t.Fatalf("expected explicit ignored external scale-down decision, got %q", decision)
	}
}

func TestBuildAutoscalingStatusDoesNotScaleDownBelowMatchingExternalIntent(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 3,
		},
		MinReplicas: 2,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{
		SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: true,
			Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
		}},
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 1,
			MaxTimerDrivenThreads:    10,
			LowPressure:              true,
		},
	})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 3 {
		t.Fatalf("expected matching external intent to hold at 3 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonNoActionableInput {
		t.Fatalf("expected no-actionable reason while external intent matches current replicas, got %q", status.Reason)
	}
	if status.External.Reason != "ExternalRecommendationSatisfied" {
		t.Fatalf("expected external recommendation satisfied reason, got %q", status.External.Reason)
	}
}

func TestBuildAutoscalingStatusBlocksWhileProgressing(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "RevisionDriftDetected",
		Message:            "Rollout is in progress",
		LastTransitionTime: metav1.Now(),
	})

	blocked, reason := blockedAutoscalingStatus(cluster)

	if !blocked {
		t.Fatalf("expected advisory autoscaling to be blocked while progressing")
	}
	if reason != autoscalingReasonProgressing {
		t.Fatalf("expected progressing block reason, got %q", reason)
	}
}

func TestBuildAutoscalingStatusPreservesExternalIntentWhileBlocked(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			RequestedReplicas: 4,
		},
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "Restoring",
		Message:            "restore is in progress",
		LastTransitionTime: metav1.Now(),
	})

	reconciler := &NiFiClusterReconciler{}
	status, _ := reconciler.buildAutoscalingStatusForTarget(ctx, cluster, managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev"))
	if status.Reason != autoscalingReasonProgressing {
		t.Fatalf("expected progressing reason, got %q", status.Reason)
	}
	if !status.External.Observed || status.External.Source != platformv1alpha1.AutoscalingExternalIntentSourceKEDA {
		t.Fatalf("expected blocked status to retain external KEDA intent, got %#v", status.External)
	}
}

func TestBlockedAutoscalingStatusTreatsRolloutTLSAndRestoreAsProgressingPrecedence(t *testing.T) {
	testCases := []string{
		"RevisionDriftDetected",
		"TLSAutoreloadObserving",
		"Restoring",
	}

	for _, progressingReason := range testCases {
		t.Run(progressingReason, func(t *testing.T) {
			cluster := managedCluster()
			cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
				Mode: platformv1alpha1.AutoscalingModeEnforced,
			}
			cluster.Status.Replicas.Desired = 3
			setAutoscalingSteadyStateConditions(cluster)
			cluster.SetCondition(metav1.Condition{
				Type:               platformv1alpha1.ConditionProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             progressingReason,
				Message:            "managed lifecycle work is in progress",
				LastTransitionTime: metav1.Now(),
			})

			blocked, reason := blockedAutoscalingStatus(cluster)
			if !blocked {
				t.Fatalf("expected autoscaling to be blocked while %s is active", progressingReason)
			}
			if reason != autoscalingReasonProgressing {
				t.Fatalf("expected progressing precedence for %s, got %q", progressingReason, reason)
			}
		})
	}
}

func TestBuildAutoscalingStatusBlocksWhileHibernatedOrDegraded(t *testing.T) {
	t.Run("hibernated", func(t *testing.T) {
		cluster := managedCluster()
		cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
			Mode: platformv1alpha1.AutoscalingModeAdvisory,
		}
		cluster.Status.Replicas.Desired = 3
		setAutoscalingSteadyStateConditions(cluster)
		cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated

		blocked, reason := blockedAutoscalingStatus(cluster)
		if !blocked {
			t.Fatalf("expected hibernated advisory autoscaling to be blocked")
		}
		if reason != autoscalingReasonHibernated {
			t.Fatalf("expected hibernated block reason, got %q", reason)
		}
	})

	t.Run("degraded", func(t *testing.T) {
		cluster := managedCluster()
		cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
			Mode: platformv1alpha1.AutoscalingModeAdvisory,
		}
		cluster.Status.Replicas.Desired = 3
		setAutoscalingSteadyStateConditions(cluster)
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionTrue,
			Reason:             "RolloutFailed",
			Message:            "Cluster is degraded",
			LastTransitionTime: metav1.Now(),
		})

		blocked, reason := blockedAutoscalingStatus(cluster)
		if !blocked {
			t.Fatalf("expected degraded advisory autoscaling to be blocked")
		}
		if reason != autoscalingReasonDegraded {
			t.Fatalf("expected degraded block reason, got %q", reason)
		}
	})
}

func TestMaybeExecuteAutoscalingScaleUpDoesNotActWhenRecommendationIsBlocked(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "RevisionDriftDetected",
		Message:            "Rollout is in progress",
		LastTransitionTime: metav1.Now(),
	})

	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	reconciler := &NiFiClusterReconciler{
		AutoscalingCollector: &fakeAutoscalingCollector{
			collection: autoscalingSignalCollection{
				SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
					Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
					Available: true,
					Message:   "queuedFlowFiles=64 queuedBytes=0 activeTimerDrivenThreads=10/10 backlog is actionable",
				}},
				QueuePressure: autoscalingQueuePressureSample{
					Observed:                 true,
					FlowFilesQueued:          64,
					BytesQueuedObserved:      true,
					ThreadCountsObserved:     true,
					ActiveTimerDrivenThreads: 10,
					MaxTimerDrivenThreads:    10,
					Actionable:               true,
				},
			},
		},
	}

	scaled, _, err := reconciler.maybeExecuteAutoscalingScaleUp(ctx, cluster, statefulSet)
	if err != nil {
		t.Fatalf("maybeExecuteAutoscalingScaleUp returned error: %v", err)
	}
	if scaled {
		t.Fatalf("expected blocked autoscaling not to scale")
	}
	if updated := derefOptionalInt32(cluster.Status.Autoscaling.RecommendedReplicas); updated != 0 {
		t.Fatalf("expected blocked autoscaling not to set a recommendation, got %d", updated)
	}
	if cluster.Status.Autoscaling.LastScalingDecision != "NoScaleUp: recommendation is unavailable because Progressing" {
		t.Fatalf("expected blocked decision, got %q", cluster.Status.Autoscaling.LastScalingDecision)
	}
}

func TestSyncAutoscalingStatusPreservesLastEvaluationTimeWhenMeaningDoesNotChange(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=2/10",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 2,
				MaxTimerDrivenThreads:    10,
			},
		},
	}

	reconciler.syncAutoscalingStatus(ctx, cluster)
	firstTime := cluster.Status.Autoscaling.LastEvaluationTime
	if firstTime == nil {
		t.Fatalf("expected first advisory evaluation time to be recorded")
	}

	reconciler.syncAutoscalingStatus(ctx, cluster)
	secondTime := cluster.Status.Autoscaling.LastEvaluationTime
	if secondTime == nil {
		t.Fatalf("expected repeated advisory evaluation time to remain set")
	}
	if !firstTime.Equal(secondTime) {
		t.Fatalf("expected advisory evaluation timestamp to remain stable when recommendation meaning is unchanged")
	}
}

func TestReconcilePopulatesAdvisoryAutoscalingStatusAtSteadyState(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=12 queuedBytes=0 activeTimerDrivenThreads=10/10 backlog is actionable",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				FlowFilesQueued:          12,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 10,
				MaxTimerDrivenThreads:    10,
				Actionable:               true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if got := derefOptionalInt32(updatedCluster.Status.Autoscaling.RecommendedReplicas); got != 4 {
		t.Fatalf("expected advisory recommendation to scale up to 4 replicas, got %d", got)
	}
	if updatedCluster.Status.Autoscaling.Reason != autoscalingReasonQueuePressure {
		t.Fatalf("expected queue-pressure reason, got %q", updatedCluster.Status.Autoscaling.Reason)
	}
	if updatedCluster.Status.Autoscaling.LastEvaluationTime == nil {
		t.Fatalf("expected advisory evaluation time to be recorded in status")
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected advisory autoscaling not to change replicas, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingScalesUpOneStep(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		MaxReplicas: 5,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=32 queuedBytes=0 activeTimerDrivenThreads=10/10 backlog is actionable",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				FlowFilesQueued:          32,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 10,
				MaxTimerDrivenThreads:    10,
				Actionable:               true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected autoscaling scale-up to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.LastScaleUpTime == nil {
		t.Fatalf("expected last scale-up time to be recorded")
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "ScaleUp:") {
		t.Fatalf("expected scale-up decision to be recorded, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 4 {
		t.Fatalf("expected enforced autoscaling to scale to 4 replicas, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingScalesUpOneStepForExternalIntent(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			RequestedReplicas: 5,
		},
		MaxReplicas: 5,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected autoscaling scale-up to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if !updatedCluster.Status.Autoscaling.External.Observed || !updatedCluster.Status.Autoscaling.External.Actionable {
		t.Fatalf("expected explicit external autoscaling status, got %#v", updatedCluster.Status.Autoscaling.External)
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "ScaleUp:") {
		t.Fatalf("expected controller scale-up decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 4 {
		t.Fatalf("expected controller to scale up one step from external intent, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingRespectsCooldown(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lastScaleUp := metav1.NewTime(time.Now().UTC())
	cluster.Status.Autoscaling.LastScaleUpTime = &lastScaleUp
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
			Cooldown: metav1.Duration{
				Duration: 10 * time.Minute,
			},
		},
		MaxReplicas: 5,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=24 queuedBytes=0 activeTimerDrivenThreads=10/10 backlog is actionable",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				FlowFilesQueued:          24,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 10,
				MaxTimerDrivenThreads:    10,
				Actionable:               true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "NoScaleUp:") {
		t.Fatalf("expected cooldown block decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected cooldown to keep replicas at %d, got %d", replicas, got)
	}
}

func TestMaybeExecuteAutoscalingScaleUpUsesLatestStatusForCooldown(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lastScaleUp := metav1.NewTime(time.Now().UTC())
	cluster.Status.Replicas.Desired = 3
	cluster.Status.Autoscaling.LastScaleUpTime = &lastScaleUp
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
			Cooldown: metav1.Duration{
				Duration: 10 * time.Minute,
			},
		},
		MinReplicas: 4,
		MaxReplicas: 5,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=24 queuedBytes=0 activeTimerDrivenThreads=10/10 backlog is actionable",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				FlowFilesQueued:          24,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 10,
				MaxTimerDrivenThreads:    10,
				Actionable:               true,
			},
		},
	}

	staleCluster := cluster.DeepCopy()
	staleCluster.InitializeConditions()
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Target StatefulSet and NiFi cluster health are converged",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "NoDrift",
		Message:            "No rollout is currently in progress and no watched drift is active",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "No degradation detected",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.Status.Autoscaling.LastScaleUpTime = nil

	scaled, _, err := reconciler.maybeExecuteAutoscalingScaleUp(ctx, staleCluster, statefulSet.DeepCopy())
	if err != nil {
		t.Fatalf("maybeExecuteAutoscalingScaleUp returned error: %v", err)
	}
	if scaled {
		t.Fatalf("expected cooldown to block scale-up when the API reader has a fresher lastScaleUpTime")
	}
	if !strings.HasPrefix(staleCluster.Status.Autoscaling.LastScalingDecision, "NoScaleUp: cooldown is") {
		t.Fatalf("expected cooldown decision from fresh status, got %q", staleCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected stale reconcile input to leave replicas at %d, got %d", replicas, got)
	}
}

func TestSyncAutoscalingStatusPreservesLatestScaleUpCooldownState(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeEnforced,
		MinReplicas: 4,
		MaxReplicas: 4,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled:  true,
			Cooldown: metav1.Duration{Duration: 5 * time.Minute},
		},
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	persistedCluster := cluster.DeepCopy()
	lastScaleUp := metav1.NewTime(time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second))
	persistedCluster.Status.Autoscaling.LastScaleUpTime = &lastScaleUp
	persistedCluster.Status.Autoscaling.LastScalingDecision = "ScaleUp: increased target StatefulSet replicas from 2 to 3"

	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{}, persistedCluster, statefulSet)

	staleCluster := persistedCluster.DeepCopy()
	staleCluster.Status.Autoscaling.LastScaleUpTime = nil
	staleCluster.Status.Autoscaling.LastScalingDecision = ""

	reconciler.syncAutoscalingStatus(ctx, staleCluster)

	if staleCluster.Status.Autoscaling.LastScaleUpTime == nil {
		t.Fatalf("expected syncAutoscalingStatus to preserve the freshest lastScaleUpTime")
	}
	if staleCluster.Status.Autoscaling.LastScaleUpTime.Unix() != lastScaleUp.Unix() {
		t.Fatalf("expected syncAutoscalingStatus to keep lastScaleUpTime %s, got %s", lastScaleUp.UTC().Format(time.RFC3339), staleCluster.Status.Autoscaling.LastScaleUpTime.UTC().Format(time.RFC3339))
	}
	if !strings.HasPrefix(staleCluster.Status.Autoscaling.LastScalingDecision, "NoScaleUp: cooldown is active until") {
		t.Fatalf("expected syncAutoscalingStatus to publish cooldown decision, got %q", staleCluster.Status.Autoscaling.LastScalingDecision)
	}
}

func TestSyncAutoscalingStatusPreservesPersistedExecutionState(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeEnforced,
		MinReplicas: 1,
		MaxReplicas: 3,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
	}
	cluster.Status.Replicas.Desired = 2
	setAutoscalingSteadyStateConditions(cluster)

	persistedCluster := cluster.DeepCopy()
	startedAt := metav1.NewTime(time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second))
	targetReplicas := int32(2)
	persistedCluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		StartedAt:      &startedAt,
		TargetReplicas: &targetReplicas,
	}
	persistedCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForAutoscalingScaleDown",
		Message:            "Waiting for autoscaling scale-down to settle",
		LastTransitionTime: metav1.Now(),
	})
	persistedCluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	statefulSet := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{}, persistedCluster, statefulSet)

	staleCluster := persistedCluster.DeepCopy()
	staleCluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{}

	reconciler.syncAutoscalingStatus(ctx, staleCluster)

	if staleCluster.Status.Autoscaling.Execution.Phase != platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle {
		t.Fatalf("expected syncAutoscalingStatus to preserve execution phase, got %#v", staleCluster.Status.Autoscaling.Execution)
	}
	if staleCluster.Status.Autoscaling.Execution.StartedAt == nil || !staleCluster.Status.Autoscaling.Execution.StartedAt.Equal(&startedAt) {
		t.Fatalf("expected syncAutoscalingStatus to preserve execution start %s, got %#v", startedAt.UTC().Format(time.RFC3339), staleCluster.Status.Autoscaling.Execution.StartedAt)
	}
	if staleCluster.Status.Autoscaling.Execution.TargetReplicas == nil || *staleCluster.Status.Autoscaling.Execution.TargetReplicas != targetReplicas {
		t.Fatalf("expected syncAutoscalingStatus to preserve execution target replicas %d, got %#v", targetReplicas, staleCluster.Status.Autoscaling.Execution.TargetReplicas)
	}
}

func TestSyncAutoscalingStatusClearsSettledScaleUpExecutionState(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-2 * time.Minute)},
		TargetReplicas: ptrTo(int32(3)),
	}
	setAutoscalingSteadyStateConditions(cluster)

	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)

	reconciler.syncAutoscalingStatus(ctx, cluster)

	if cluster.Status.Autoscaling.Execution.Phase != "" {
		t.Fatalf("expected settled scale-up execution state to clear, got %#v", cluster.Status.Autoscaling.Execution)
	}
}

func TestReconcileEnforcedAutoscalingClampsToOneStepFromMinimumRecommendation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		MinReplicas: 4,
		MaxReplicas: 6,
	}

	replicas := int32(1)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(1)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected one-step scale-up to 2 replicas, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingNeverScalesDown(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 2,
	}

	replicas := int32(4)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(4)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.LastScalingDecision != "NoScaleDown: scale-down is not enabled" {
		t.Fatalf("expected scale-down-disabled decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected replicas to remain %d, got %d", replicas, got)
	}
}

func TestLiveAutoscalingSignalCollectorCollectsRealNiFiSignals(t *testing.T) {
	sts, _, kubeClient := nodeManagerFixtures(t)

	collector := &LiveAutoscalingSignalCollector{
		KubeClient: kubeClient,
		NiFiClient: &fakeNiFiClient{
			rootStatus: nifi.RootProcessGroupStatus{
				FlowFilesQueued:     48,
				BytesQueued:         2097152,
				BytesQueuedObserved: true,
			},
			systemDiagnostics: nifi.SystemDiagnostics{
				ActiveTimerDrivenThreads: 10,
				MaxTimerDrivenThreads:    10,
				ThreadCountsObserved:     true,
				CPULoadAverage:           1.5,
				CPULoadObserved:          true,
				AvailableProcessors:      2,
			},
		},
	}

	collection := collector.Collect(context.Background(), managedCluster(), sts, []platformv1alpha1.AutoscalingSignal{
		platformv1alpha1.AutoscalingSignalQueuePressure,
		platformv1alpha1.AutoscalingSignalCPU,
	})

	if !collection.QueuePressure.Actionable {
		t.Fatalf("expected queue-pressure sample to be actionable, got %+v", collection.QueuePressure)
	}
	if !collection.CPU.Observed {
		t.Fatalf("expected CPU sample to be observed, got %+v", collection.CPU)
	}
	if len(collection.SignalStatuses) != 2 {
		t.Fatalf("expected two signal statuses, got %#v", collection.SignalStatuses)
	}
	if !collection.SignalStatuses[0].Available || !collection.SignalStatuses[1].Available {
		t.Fatalf("expected collected signal statuses to be available, got %#v", collection.SignalStatuses)
	}
}

func TestBuildAutoscalingStatusRecommendsScaleDownForLowPressure(t *testing.T) {
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Status.Replicas.Desired = 3
	setAutoscalingSteadyStateConditions(cluster)

	status := buildAutoscalingSteadyStateStatus(cluster, cluster.Spec.Autoscaling, autoscalingSignalCollection{
		SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: true,
			Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
		}},
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 1,
			MaxTimerDrivenThreads:    10,
			LowPressure:              true,
		},
	})

	if got := derefOptionalInt32(status.RecommendedReplicas); got != 2 {
		t.Fatalf("expected low pressure to recommend 2 replicas, got %d", got)
	}
	if status.Reason != autoscalingReasonLowPressure {
		t.Fatalf("expected low-pressure reason, got %q", status.Reason)
	}
}

func TestUpdatedAutoscalingLowPressureSincePreservesPreviousDuringProgressing(t *testing.T) {
	previous := platformv1alpha1.AutoscalingStatus{
		LowPressureSince: &metav1.Time{Time: time.Now().UTC().Add(-5 * time.Minute)},
	}
	desired := platformv1alpha1.AutoscalingStatus{
		Reason: autoscalingReasonProgressing,
	}

	preserved := updatedAutoscalingLowPressureSince(previous, desired, autoscalingSignalCollection{})
	if preserved == nil {
		t.Fatalf("expected low pressure timestamp to be preserved during progressing")
	}
	if !preserved.Equal(previous.LowPressureSince) {
		t.Fatalf("expected preserved low pressure timestamp %s, got %s", previous.LowPressureSince.Time, preserved.Time)
	}
}

func TestUpdatedAutoscalingLowPressureStatusCountsConsecutiveSamples(t *testing.T) {
	previousObservedAt := metav1.NewTime(time.Now().UTC().Add(-10 * time.Second))
	previous := platformv1alpha1.AutoscalingStatus{
		LastEvaluationTime: &previousObservedAt,
		LowPressure: platformv1alpha1.AutoscalingLowPressureStatus{
			Since:                      &previousObservedAt,
			LastObservedAt:             &previousObservedAt,
			ConsecutiveSamples:         2,
			RequiredConsecutiveSamples: 3,
		},
		LowPressureSince: &previousObservedAt,
	}
	currentObservedAt := metav1.NewTime(time.Now().UTC())
	desired := platformv1alpha1.AutoscalingStatus{
		Reason:             autoscalingReasonLowPressure,
		LastEvaluationTime: &currentObservedAt,
	}

	status := updatedAutoscalingLowPressureStatus(previous, desired, autoscalingSignalCollection{
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 1,
			MaxTimerDrivenThreads:    10,
			LowPressure:              true,
		},
	})

	if status.ConsecutiveSamples != 3 {
		t.Fatalf("expected low-pressure evidence to cap at 3 consecutive samples, got %+v", status)
	}
	if status.RequiredConsecutiveSamples != 3 {
		t.Fatalf("expected required sample count to remain 3, got %+v", status)
	}
	if status.Since == nil || !status.Since.Equal(&previousObservedAt) {
		t.Fatalf("expected low-pressure evidence to preserve its first observation time, got %+v", status)
	}
}

func TestUpdatedAutoscalingLowPressureStatusRequiresExtraSamplesWhenEvidenceIsIncomplete(t *testing.T) {
	observedAt := metav1.NewTime(time.Now().UTC())
	status := updatedAutoscalingLowPressureStatus(platformv1alpha1.AutoscalingStatus{}, platformv1alpha1.AutoscalingStatus{
		Reason:             autoscalingReasonLowPressure,
		LastEvaluationTime: &observedAt,
	}, autoscalingSignalCollection{
		QueuePressure: autoscalingQueuePressureSample{
			Observed:            true,
			BytesQueuedObserved: false,
			LowPressure:         true,
		},
	})

	if status.RequiredConsecutiveSamples != 7 {
		t.Fatalf("expected incomplete low-pressure evidence to require 7 samples, got %+v", status)
	}
	if !strings.Contains(status.Message, "queued bytes unavailable") {
		t.Fatalf("expected low-pressure message to explain missing queued-bytes evidence, got %q", status.Message)
	}
	if !strings.Contains(status.Message, "thread counts unavailable") {
		t.Fatalf("expected low-pressure message to explain missing thread-count evidence, got %q", status.Message)
	}
}

func TestUpdatedAutoscalingLowPressureStatusPreservesStricterRequiredSamplesUntilReset(t *testing.T) {
	previousObservedAt := metav1.NewTime(time.Now().UTC().Add(-10 * time.Second))
	currentObservedAt := metav1.NewTime(time.Now().UTC())
	previous := platformv1alpha1.AutoscalingStatus{
		LastEvaluationTime: &previousObservedAt,
		LowPressure: platformv1alpha1.AutoscalingLowPressureStatus{
			Since:                      &previousObservedAt,
			LastObservedAt:             &previousObservedAt,
			ConsecutiveSamples:         4,
			RequiredConsecutiveSamples: 7,
		},
	}

	status := updatedAutoscalingLowPressureStatus(previous, platformv1alpha1.AutoscalingStatus{
		Reason:             autoscalingReasonLowPressure,
		LastEvaluationTime: &currentObservedAt,
	}, autoscalingSignalCollection{
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 1,
			MaxTimerDrivenThreads:    10,
			LowPressure:              true,
		},
	})

	if status.RequiredConsecutiveSamples != 7 {
		t.Fatalf("expected stricter low-pressure sample barrier to persist until low pressure resets, got %+v", status)
	}
	if status.ConsecutiveSamples != 5 {
		t.Fatalf("expected another qualifying sample to increase consecutive evidence, got %+v", status)
	}
}

func TestAutoscalingLowPressureObservedRejectsZeroBacklogWithBusyExecutorActivity(t *testing.T) {
	samples := autoscalingSignalCollection{
		QueuePressure: autoscalingQueuePressureSample{
			Observed:                 true,
			FlowFilesQueued:          0,
			BytesQueuedObserved:      true,
			ThreadCountsObserved:     true,
			ActiveTimerDrivenThreads: 4,
			MaxTimerDrivenThreads:    10,
			LowPressure:              true,
		},
	}

	if autoscalingLowPressureObserved(samples) {
		t.Fatalf("expected busy executor activity to reject low-pressure evidence")
	}
	if got := autoscalingLowPressureBlockedReason(samples); !strings.Contains(got, "activeTimerDrivenThreads=4/10") {
		t.Fatalf("expected blocked reason to explain the busy executor activity, got %q", got)
	}
}

func TestReconcileAdvisoryAutoscalingNeverScalesDown(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if got := derefOptionalInt32(updatedCluster.Status.Autoscaling.RecommendedReplicas); got != 2 {
		t.Fatalf("expected advisory low pressure to recommend 2 replicas, got %d", got)
	}
	if updatedCluster.Status.Autoscaling.LastScalingDecision != "NoScale: autoscaling is not in enforced mode" {
		t.Fatalf("expected advisory status-only decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected advisory autoscaling not to change replicas, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingScalesDownOneStepAfterSustainedLowPressure(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lowPressureSince := metav1.NewTime(time.Now().UTC().Add(-10 * time.Minute))
	cluster.Status.Autoscaling.LowPressureSince = &lowPressureSince
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected autoscaling scale-down to requeue, got %s", result.RequeueAfter)
	}
	if result.Requeue {
		t.Fatalf("expected autoscaling scale-down to use timed requeue only")
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.LastScaleDownTime == nil {
		t.Fatalf("expected last scale-down time to be recorded")
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "ScaleDown:") {
		t.Fatalf("expected scale-down decision to be recorded, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected enforced autoscaling to scale down to 2 replicas, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingScalesDownOneStepForExternalIntent(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lowPressureSince := metav1.NewTime(time.Now().UTC().Add(-10 * time.Minute))
	cluster.Status.Autoscaling.LowPressureSince = &lowPressureSince
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 1,
		},
		MinReplicas: 2,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected autoscaling scale-down to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if !strings.Contains(updatedCluster.Status.Autoscaling.LastScalingDecision, "ScaleDown:") {
		t.Fatalf("expected external scale-down decision to be recorded, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}
	if got := derefOptionalInt32(updatedCluster.Status.Autoscaling.External.RequestedReplicas); got != 1 {
		t.Fatalf("expected external requested replicas to stay visible during scale-down, got %d", got)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected external downscale intent to reduce replicas to 2, got %d", got)
	}
}

func TestReconcileEnforcedAutoscalingBlocksExternalScaleDownWithoutLowPressure(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 2,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=2/10 backlog is idle",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:            true,
				BytesQueuedObserved: true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Reason != autoscalingReasonExternalScaleDown {
		t.Fatalf("expected external scale-down reason, got %q", updatedCluster.Status.Autoscaling.Reason)
	}
	if updatedCluster.Status.Autoscaling.LastScalingDecision != "NoScaleDown: low pressure is not currently observed" {
		t.Fatalf("expected low-pressure block for external scale-down, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected blocked external scale-down to keep replicas at %d, got %d", replicas, got)
	}
}

func TestReconcileEnforcedAutoscalingDoesNotScaleDownBelowMatchingExternalIntent(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lowPressureSince := metav1.NewTime(time.Now().UTC().Add(-10 * time.Minute))
	cluster.Status.Autoscaling.LowPressureSince = &lowPressureSince
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 3,
		},
		MinReplicas: 2,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.LastScalingDecision != "NoScale: recommended replicas are already satisfied" {
		t.Fatalf("expected matching external intent to block scale-down, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected matching external intent to keep replicas at %d, got %d", replicas, got)
	}
}

func TestReconcileEnforcedAutoscalingScaleDownRespectsStabilizationWindow(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lowPressureSince := metav1.NewTime(time.Now().UTC().Add(-30 * time.Second))
	cluster.Status.Autoscaling.LowPressureSince = &lowPressureSince
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "NoScaleDown: low pressure must remain stable until") {
		t.Fatalf("expected stabilization block decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected stabilization to keep replicas at %d, got %d", replicas, got)
	}
}

func TestReconcileEnforcedAutoscalingRejectsTransientZeroBacklogDip(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=4/10 backlog is zero but active timer-driven work is still above the low-pressure threshold 2",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 4,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if got := updatedCluster.Status.Autoscaling.LastScalingDecision; !strings.Contains(got, "zero backlog is not yet trustworthy") {
		t.Fatalf("expected transient zero-backlog dip to be rejected explicitly, got %q", got)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected transient zero-backlog dip to keep replicas at %d, got %d", replicas, got)
	}
}

func TestMaybeExecuteAutoscalingScaleDownUsesLatestStatusForCooldown(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lastScaleDown := metav1.NewTime(time.Now().UTC())
	cluster.Status.Replicas.Desired = 3
	cluster.Status.Autoscaling.LowPressureSince = &metav1.Time{Time: time.Now().UTC().Add(-10 * time.Minute)}
	cluster.Status.Autoscaling.LastScaleDownTime = &lastScaleDown
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 2 * time.Minute},
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:                 true,
				BytesQueuedObserved:      true,
				ThreadCountsObserved:     true,
				ActiveTimerDrivenThreads: 1,
				MaxTimerDrivenThreads:    10,
				LowPressure:              true,
			},
		},
	}

	staleCluster := cluster.DeepCopy()
	staleCluster.InitializeConditions()
	staleCluster.Status.Replicas.Desired = 3
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Target StatefulSet and NiFi cluster health are converged",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "NoDrift",
		Message:            "No rollout is currently in progress and no watched drift is active",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "No degradation detected",
		LastTransitionTime: metav1.Now(),
	})
	staleCluster.Status.Autoscaling.LastScaleDownTime = nil

	scaled, _, err := reconciler.maybeExecuteAutoscalingScaleDown(ctx, staleCluster, statefulSet.DeepCopy(), pods)
	if err != nil {
		t.Fatalf("maybeExecuteAutoscalingScaleDown returned error: %v", err)
	}
	if scaled {
		t.Fatalf("expected cooldown to block scale-down when the API reader has a fresher lastScaleDownTime")
	}
	if !strings.HasPrefix(staleCluster.Status.Autoscaling.LastScalingDecision, "NoScaleDown: cooldown is") {
		t.Fatalf("expected cooldown decision from fresh status, got %q", staleCluster.Status.Autoscaling.LastScalingDecision)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected stale reconcile input to leave replicas at %d, got %d", replicas, got)
	}
}

func TestReconcileAutoscalingScaleDownResumesInProgressNodePreparation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose: platformv1alpha1.NodeOperationPurposeScaleDown,
		PodName: "nifi-2",
		PodUID:  "uid-2",
		NodeID:  "node-2",
		Stage:   platformv1alpha1.NodeOperationStageOffloading,
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "PreparingNodeForScaleDown",
		Message:            "Waiting for NiFi node offload",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for NiFi node offload")
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].UID = "uid-2"
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{readyImmediately: true}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected in-progress scale-down to requeue, got %s", result.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected resumed scale-down to reduce replicas to 2, got %d", got)
	}
}

func TestReconcileAutoscalingScaleDownFailsWhenNodePreparationTimesOut(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	lowPressureSince := metav1.NewTime(time.Now().UTC().Add(-5 * time.Minute))
	cluster.Status.Autoscaling.LowPressureSince = &lowPressureSince
	cluster.Spec.Hibernation.OffloadTimeout = metav1.Duration{Duration: 30 * time.Second}
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			StabilizationWindow: metav1.Duration{Duration: time.Second},
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{{
			result: NodePreparationResult{
				TimedOut: true,
				Message:  "timed out waiting for NiFi node node-2 to reach OFFLOADED before proceeding",
				Operation: platformv1alpha1.NodeOperationStatus{
					Purpose: platformv1alpha1.NodeOperationPurposeScaleDown,
					PodName: "nifi-2",
					PodUID:  string(pods[2].UID),
					NodeID:  "node-2",
					Stage:   platformv1alpha1.NodeOperationStageOffloading,
				},
			},
		}},
	}
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 activeTimerDrivenThreads=1/10 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:            true,
				BytesQueuedObserved: true,
				LowPressure:         true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected timed-out scale-down to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if condition := updatedCluster.GetCondition(platformv1alpha1.ConditionDegraded); condition == nil || condition.Reason != "NodePreparationTimedOut" {
		t.Fatalf("expected node-preparation timeout degradation, got %#v", condition)
	}
	if updatedCluster.Status.Autoscaling.Execution.Phase != platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare {
		t.Fatalf("expected timed-out scale-down to keep prepare execution state, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if updatedCluster.Status.Autoscaling.Execution.State != platformv1alpha1.AutoscalingExecutionStateBlocked {
		t.Fatalf("expected timed-out scale-down to stay blocked, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownOffloadTimedOut {
		t.Fatalf("expected timed-out scale-down to report the timeout reason, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if got := updatedCluster.Status.Autoscaling.LastScalingDecision; !strings.Contains(got, "exceeded the configured preparation timeout") {
		t.Fatalf("expected timed-out scale-down to publish operator guidance, got %q", got)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != replicas {
		t.Fatalf("expected timed-out scale-down to keep replicas at %d, got %d", replicas, got)
	}
}

func TestReconcileAutoscalingScaleDownMarksOffloadRetryingAsBlocked(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose: platformv1alpha1.NodeOperationPurposeScaleDown,
		PodName: "nifi-2",
		PodUID:  "uid-2",
		NodeID:  "node-2",
		Stage:   platformv1alpha1.NodeOperationStageOffloading,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  autoscalingBlockedReasonScaleDownOffloadRetrying,
		Message:        "Waiting for NiFi node offload",
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "PreparingNodeForScaleDown",
		Message:            "Waiting for NiFi node offload",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for NiFi node offload")
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].UID = "uid-2"
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{{
			err: fmt.Errorf("temporary NiFi lifecycle API error"),
		}},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected retrying scale-down to requeue, got %#v", result)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownOffloadRetrying {
		t.Fatalf("expected offload retry reason, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if got := updatedCluster.Status.Autoscaling.LastScalingDecision; !strings.Contains(got, "controller will retry from the same highest ordinal pod") {
		t.Fatalf("expected retry guidance in last scaling decision, got %q", got)
	}
}

func TestWaitForAutoscalingScaleDownStepToSettleIgnoresStalePodListWhenStatefulSetIsSettled(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResultWithPods("nifi", 2)}},
	}, cluster, target)

	settled, result, err := reconciler.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("waitForAutoscalingScaleDownStepToSettle returned error: %v", err)
	}
	if !settled {
		t.Fatalf("expected settled scale-down step, got result %#v", result)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("expected no requeue result, got %#v", result)
	}
}

func TestWaitForAutoscalingScaleDownStepToSettleUsesScaleDownHealthGate(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	health := healthResponse{
		result: ClusterHealthResult{
			ExpectedReplicas: 2,
			ReadyPods:        2,
			ReachablePods:    2,
			ConvergedPods:    0,
			Pods: []PodHealth{
				{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
				{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
			},
		},
	}
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{health},
	}, cluster, target)

	settled, result, err := reconciler.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("waitForAutoscalingScaleDownStepToSettle returned error: %v", err)
	}
	if !settled {
		t.Fatalf("expected scale-down health gate to accept 2/3 post-removal convergence, got result %#v", result)
	}
	if result != (ctrl.Result{}) {
		t.Fatalf("expected no requeue result, got %#v", result)
	}
}

func TestWaitForAutoscalingScaleDownStepToSettleBoundsHealthCheck(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-2 * time.Minute)}

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	var observedTimeout time.Duration
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkFn: func(ctx context.Context, _ *platformv1alpha1.NiFiCluster, _ *appsv1.StatefulSet) (ClusterHealthResult, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatalf("expected bounded autoscaling settle health-check context")
			}
			observedTimeout = time.Until(deadline)
			<-ctx.Done()
			return ClusterHealthResult{ExpectedReplicas: 2}, ctx.Err()
		},
	}, cluster, target)

	settled, result, err := reconciler.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("waitForAutoscalingScaleDownStepToSettle returned error: %v", err)
	}
	if settled {
		t.Fatalf("expected timed health check to block settlement")
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected timed health check to requeue after %s, got %#v", rolloutPollRequeue, result)
	}
	if observedTimeout <= 0 || observedTimeout > 2*time.Minute {
		t.Fatalf("expected bounded health-check timeout, got %s", observedTimeout)
	}
	if cluster.Status.Autoscaling.Execution.State != platformv1alpha1.AutoscalingExecutionStateBlocked {
		t.Fatalf("expected blocked execution state after timed health check, got %#v", cluster.Status.Autoscaling.Execution)
	}
	if cluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownHealthGateBlocked {
		t.Fatalf("expected health gate blocked reason, got %#v", cluster.Status.Autoscaling.Execution)
	}
	if cluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseRunning {
		t.Fatalf("expected running last operation while waiting for bounded health retry, got %#v", cluster.Status.LastOperation)
	}
}

func TestWaitForAutoscalingScaleDownStepToSettleMarksDrainStalledAfterTimeout(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Hibernation.OffloadTimeout = metav1.Duration{Duration: 30 * time.Second}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-2 * time.Minute)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  autoscalingBlockedReasonScaleDownDrainPending,
		Message:        "Waiting for the previous autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-2 * time.Minute)}

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].DeletionTimestamp = ptrTo(metav1.Now())
	pods[2].Finalizers = []string{"test"}

	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{}, cluster, target)

	settled, result, err := reconciler.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("waitForAutoscalingScaleDownStepToSettle returned error: %v", err)
	}
	if settled {
		t.Fatalf("expected stalled drain to block settlement")
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected stalled drain to requeue, got %#v", result)
	}
	if cluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownDrainStalled {
		t.Fatalf("expected drain stall reason, got %#v", cluster.Status.Autoscaling.Execution)
	}
	if degraded := cluster.GetCondition(platformv1alpha1.ConditionDegraded); degraded == nil || degraded.Reason != autoscalingBlockedReasonScaleDownDrainStalled || degraded.Status != metav1.ConditionTrue {
		t.Fatalf("expected degraded drain stall condition, got %#v", degraded)
	}
	if got := cluster.Status.Autoscaling.LastScalingDecision; !strings.Contains(got, "pod termination or replica settlement has stalled") {
		t.Fatalf("expected stalled drain guidance in last scaling decision, got %q", got)
	}
}

func TestWaitForAutoscalingScaleDownStepToSettleRequiresPostScaleDownStabilizationDelay(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC()}

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	healthChecker := &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResultWithPods("nifi", 2)}},
	}
	reconciler, _ := newTestReconciler(t, healthChecker, cluster, target)

	settled, result, err := reconciler.waitForAutoscalingScaleDownStepToSettle(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("waitForAutoscalingScaleDownStepToSettle returned error: %v", err)
	}
	if settled {
		t.Fatalf("expected recent scale-down to delay settlement")
	}
	if result.RequeueAfter <= 0 || result.RequeueAfter > rolloutPollRequeue {
		t.Fatalf("expected a bounded stabilization requeue, got %#v", result)
	}
	if healthChecker.checkCalls != 0 {
		t.Fatalf("expected stabilization delay to avoid immediate health sampling, got %d calls", healthChecker.checkCalls)
	}
}

func TestReconcileAutoscalingScaleDownSettledPublishesCooldownDecision(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 30 * time.Second},
		},
		MinReplicas: 1,
		MaxReplicas: 3,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.Replicas.Desired = 2
	cluster.Status.Replicas.Ready = 2
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.Status.Autoscaling.LowPressureSince = &metav1.Time{Time: time.Now().UTC().Add(-5 * time.Minute)}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-15 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForAutoscalingScaleDown",
		Message:            "Waiting for autoscaling scale-down to settle",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	health := healthResponse{
		result: ClusterHealthResult{
			ExpectedReplicas: 2,
			ReadyPods:        2,
			ReachablePods:    2,
			ConvergedPods:    0,
			Pods: []PodHealth{
				{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
				{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
			},
		},
	}
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{health},
	}, cluster, target, &pods[0], &pods[1])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:            true,
				BytesQueuedObserved: true,
				LowPressure:         true,
			},
		},
	}

	result, err := reconciler.reconcileAutoscalingScaleDown(ctx, cluster, target, pods)
	if err != nil {
		t.Fatalf("reconcileAutoscalingScaleDown returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected settled scale-down to resume steady-state polling, got %#v", result)
	}
	if cluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected settled scale-down to mark success, got %#v", cluster.Status.LastOperation)
	}
	if cluster.Status.Autoscaling.Execution.Phase != "" {
		t.Fatalf("expected settled scale-down to clear execution state, got %#v", cluster.Status.Autoscaling.Execution)
	}
	reconciler.syncAutoscalingStatus(ctx, cluster)
	if !strings.HasPrefix(cluster.Status.Autoscaling.LastScalingDecision, "NoScaleDown: cooldown is active until") {
		t.Fatalf("expected settled scale-down to publish cooldown decision, got %q", cluster.Status.Autoscaling.LastScalingDecision)
	}
}

func TestReconcileAutoscalingScaleDownResumesFromPersistedSettleExecution(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 30 * time.Second},
		},
		MinReplicas: 1,
		MaxReplicas: 3,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.Replicas.Desired = 2
	cluster.Status.Replicas.Ready = 2
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.Status.Autoscaling.LowPressureSince = &metav1.Time{Time: time.Now().UTC().Add(-5 * time.Minute)}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-15 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Cluster is healthy",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	health := healthResponse{
		result: ClusterHealthResult{
			ExpectedReplicas: 2,
			ReadyPods:        2,
			ReachablePods:    2,
			ConvergedPods:    0,
			Pods: []PodHealth{
				{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
				{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
			},
		},
	}
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{health},
	}, cluster, target, &pods[0], &pods[1])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:            true,
				BytesQueuedObserved: true,
				LowPressure:         true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected persisted settle execution to resume steady-state polling, got %#v", result)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.Phase != "" {
		t.Fatalf("expected persisted settle execution to clear, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
}

func TestReconcileAutoscalingScaleDownResumesFromPersistedExternalSettleExecution(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 30 * time.Second},
		},
		External: platformv1alpha1.AutoscalingExternalPolicy{
			Enabled:           true,
			Source:            platformv1alpha1.AutoscalingExternalIntentSourceKEDA,
			ScaleDownEnabled:  true,
			RequestedReplicas: 1,
		},
		MinReplicas: 1,
		MaxReplicas: 3,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.Replicas.Desired = 2
	cluster.Status.Replicas.Ready = 2
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.Status.Autoscaling.LowPressureSince = &metav1.Time{Time: time.Now().UTC().Add(-5 * time.Minute)}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-15 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Cluster is healthy",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	health := healthResponse{
		result: ClusterHealthResult{
			ExpectedReplicas: 2,
			ReadyPods:        2,
			ReachablePods:    2,
			ConvergedPods:    0,
			Pods: []PodHealth{
				{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
				{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
			},
		},
	}
	reconciler, _ := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{health},
	}, cluster, target, &pods[0], &pods[1])
	reconciler.AutoscalingCollector = &fakeAutoscalingCollector{
		collection: autoscalingSignalCollection{
			SignalStatuses: []platformv1alpha1.AutoscalingSignalStatus{{
				Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
				Available: true,
				Message:   "queuedFlowFiles=0 queuedBytes=0 backlog is low",
			}},
			QueuePressure: autoscalingQueuePressureSample{
				Observed:            true,
				BytesQueuedObserved: true,
				LowPressure:         true,
			},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected persisted external settle execution to resume steady-state polling, got %#v", result)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := reconciler.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.Phase != "" {
		t.Fatalf("expected persisted external settle execution to clear, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
}

func TestReconcileAutoscalingScaleDownSettleDoesNotRepeatDestructiveWorkAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Safety.RequireClusterHealthy = true
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled:             true,
			Cooldown:            metav1.Duration{Duration: 10 * time.Minute},
			StabilizationWindow: metav1.Duration{Duration: 30 * time.Second},
		},
		MinReplicas: 1,
		MaxReplicas: 3,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-30 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  "WaitingForAutoscalingScaleDown",
		Message:        "Waiting for the previous autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.Status.Autoscaling.LowPressureSince = &metav1.Time{Time: time.Now().UTC().Add(-5 * time.Minute)}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForAutoscalingScaleDown",
		Message:            "Waiting for autoscaling scale-down to settle",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}
	health := healthResponse{
		result: ClusterHealthResult{
			ExpectedReplicas: 2,
			ReadyPods:        2,
			ReachablePods:    2,
			ConvergedPods:    0,
			Pods: []PodHealth{
				{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
				{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 2, TotalNodeCount: 3},
			},
		},
	}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{health},
	}, cluster, target, &pods[0], &pods[1])
	nodeManager := &fakeNodeManager{readyImmediately: true}
	reconciler.NodeManager = nodeManager

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if nodeManager.calls != 0 {
		t.Fatalf("expected persisted settle execution not to re-run node preparation, got %d calls", nodeManager.calls)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(target), updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected persisted settle execution not to patch replicas again, got %d", got)
	}
}

func TestReconcileAutoscalingScaleDownKeepsBlockedDrainResumableAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 3,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-30 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  autoscalingBlockedReasonScaleDownDrainPending,
		Message:        "Waiting for the previous autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Now().UTC().Add(-20 * time.Second)}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForAutoscalingScaleDown",
		Message:            "Waiting for autoscaling scale-down to settle",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down to settle")

	target := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	target.Status.Replicas = 2
	target.Status.ReadyReplicas = 2
	target.Status.CurrentReplicas = 2
	target.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].DeletionTimestamp = ptrTo(metav1.Now())
	pods[2].Finalizers = []string{"test"}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, target, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{readyImmediately: true}
	reconciler.NodeManager = nodeManager

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if nodeManager.calls != 0 {
		t.Fatalf("expected blocked drain resume not to re-run node preparation, got %d calls", nodeManager.calls)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.Phase != platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle {
		t.Fatalf("expected settle execution to remain active, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownDrainPending {
		t.Fatalf("expected blocked drain reason to remain resumable, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
}

func TestReconcileAutoscalingScaleDownResumesBlockedOffloadAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose: platformv1alpha1.NodeOperationPurposeScaleDown,
		PodName: "nifi-2",
		PodUID:  "uid-2",
		NodeID:  "node-2",
		Stage:   platformv1alpha1.NodeOperationStageOffloading,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-45 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  autoscalingBlockedReasonScaleDownOffloadTimedOut,
		Message:        "Waiting for NiFi node offload",
	}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "PreparingNodeForScaleDown",
		Message:            "Waiting for NiFi node offload",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for NiFi node offload")
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].UID = "uid-2"
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{readyImmediately: true}
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected resumed blocked offload to requeue, got %#v", result)
	}
	if nodeManager.calls != 1 {
		t.Fatalf("expected one resumed node-preparation call, got %d", nodeManager.calls)
	}
	if len(nodeManager.currentStates) != 1 || nodeManager.currentStates[0].Stage != platformv1alpha1.NodeOperationStageOffloading {
		t.Fatalf("expected resume to use persisted offload state, got %#v", nodeManager.currentStates)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected resumed blocked offload to reduce replicas to 2, got %d", got)
	}
}

func TestReconcileAutoscalingScaleDownDropsStaleNodeOperationAfterPodChurn(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	setAutoscalingSteadyStateConditions(cluster)
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose: platformv1alpha1.NodeOperationPurposeScaleDown,
		PodName: "nifi-2",
		PodUID:  "old-uid",
		NodeID:  "node-2",
		Stage:   platformv1alpha1.NodeOperationStageOffloading,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownPrepare,
		State:          platformv1alpha1.AutoscalingExecutionStateBlocked,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-30 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		BlockedReason:  autoscalingBlockedReasonScaleDownOffloadRetrying,
		Message:        "Waiting for NiFi node offload",
	}
	cluster.Status.Autoscaling.LastScalingDecision = "NoScaleDown: waiting for NiFi node offload"
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[2].UID = "new-uid"

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{readyImmediately: true}
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected stale-node-operation recovery to requeue, got %#v", result)
	}
	if nodeManager.calls != 1 {
		t.Fatalf("expected one fresh node-preparation call after pod churn, got %d", nodeManager.calls)
	}
	if len(nodeManager.currentStates) != 1 || nodeManager.currentStates[0].PodName != "" {
		t.Fatalf("expected stale autoscaling node state to be cleared before resume, got %#v", nodeManager.currentStates)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: statefulSet.Name}, updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected fresh preparation after pod churn to reduce replicas to 2, got %d", got)
	}
}

type fakeAutoscalingCollector struct {
	collection autoscalingSignalCollection
}

func (f *fakeAutoscalingCollector) Collect(context.Context, *platformv1alpha1.NiFiCluster, *appsv1.StatefulSet, []platformv1alpha1.AutoscalingSignal) autoscalingSignalCollection {
	return f.collection
}

func setAutoscalingSteadyStateConditions(cluster *platformv1alpha1.NiFiCluster) {
	cluster.InitializeConditions()
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Cluster is healthy",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "NoDrift",
		Message:            "No work is in progress",
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
}
