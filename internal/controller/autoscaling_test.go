package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "NoScaleUp: cooldown is") {
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
	if updatedCluster.Status.Autoscaling.LastScalingDecision != "NoScaleUp: automatic scale-down is disabled" {
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
