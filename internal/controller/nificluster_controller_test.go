package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
	"github.com/michaelhutchings-napier/NiFi-Fabric/internal/nifi"
)

func TestBuildRolloutPlanSelectsHighestOrdinalOutdatedPod(t *testing.T) {
	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-new"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{})
	next := plan.NextPodToDelete()
	if next == nil {
		t.Fatalf("expected a pod to delete")
	}
	if next.Name != "nifi-2" {
		t.Fatalf("expected highest ordinal outdated pod, got %s", next.Name)
	}
}

func TestBuildRolloutPlanSkipsCompletedPodsDuringConfigDriftRestart(t *testing.T) {
	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	startedAt := metav1.NewTime(time.Date(2026, 3, 8, 18, 0, 0, 0, time.UTC))
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pods[0].CreationTimestamp = metav1.NewTime(startedAt.Add(-2 * time.Minute))
	pods[1].CreationTimestamp = metav1.NewTime(startedAt.Add(-90 * time.Second))
	pods[2].CreationTimestamp = metav1.NewTime(startedAt.Add(45 * time.Second))

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{
		Trigger:       platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:     &startedAt,
		CompletedPods: []string{"nifi-2"},
	})

	if len(plan.OutdatedPods) != 2 {
		t.Fatalf("expected two remaining outdated pods, got %d", len(plan.OutdatedPods))
	}
	next := plan.NextPodToDelete()
	if next == nil {
		t.Fatalf("expected a pod to delete")
	}
	if next.Name != "nifi-1" {
		t.Fatalf("expected highest remaining ordinal outdated pod, got %s", next.Name)
	}
}

func TestBuildRolloutPlanUsesPodRevisionLabelsDuringOnDeleteRollout(t *testing.T) {
	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	statefulSet.Status.CurrentReplicas = 3
	statefulSet.Status.UpdatedReplicas = 1
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{})
	next := plan.NextPodToDelete()
	if next == nil {
		t.Fatalf("expected a pod to delete")
	}
	if next.Name != "nifi-1" {
		t.Fatalf("expected next outdated pod to be nifi-1, got %s", next.Name)
	}
}

func TestBuildRolloutPlanUsesPinnedTargetRevisionDuringManagedRollout(t *testing.T) {
	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-newer")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		TargetRevision: "nifi-new",
		CompletedPods:  []string{"nifi-2"},
	})

	next := plan.NextPodToDelete()
	if next == nil {
		t.Fatalf("expected a pod to delete")
	}
	if next.Name != "nifi-1" {
		t.Fatalf("expected next outdated pod to be nifi-1, got %s", next.Name)
	}
}

func TestReconcileDeletesHighestOrdinalPodWhenRevisionDrifts(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after pod deletion, got %s", result.RequeueAfter)
	}

	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{})
	if err == nil {
		t.Fatalf("expected nifi-2 to be deleted")
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if condition := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected progressing condition true")
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseRunning {
		t.Fatalf("expected running last operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
	if updatedCluster.Status.Rollout.Trigger != platformv1alpha1.RolloutTriggerStatefulSetRevision {
		t.Fatalf("expected revision rollout trigger, got %q", updatedCluster.Status.Rollout.Trigger)
	}
	if updatedCluster.Status.Rollout.TargetRevision != "nifi-new" {
		t.Fatalf("expected target revision to be pinned, got %q", updatedCluster.Status.Rollout.TargetRevision)
	}
}

func TestReconcileBlocksRolloutWhenHealthGateFails(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses: []healthResponse{{
			result: ClusterHealthResult{ExpectedReplicas: 3, ReadyPods: 3, ReachablePods: 3, ConvergedPods: 1},
			err:    errors.New("cluster health gate not yet satisfied"),
		}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while waiting on health gate, got %s", result.RequeueAfter)
	}

	for _, podName := range []string{"nifi-0", "nifi-1", "nifi-2"} {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, pod); err != nil {
			t.Fatalf("expected pod %s to remain present: %v", podName, err)
		}
	}
}

func TestReconcileAdvancesOnePodAtATime(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{nil, errors.New("waiting for replacement pod")},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}

	if healthChecker.podReadyCalls != 2 {
		t.Fatalf("expected two readiness checks, got %d", healthChecker.podReadyCalls)
	}

	deletedPods := 0
	for _, podName := range []string{"nifi-0", "nifi-1", "nifi-2"} {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, pod); err != nil {
			deletedPods++
		}
	}
	if deletedPods != 1 {
		t.Fatalf("expected exactly one pod deletion, got %d", deletedPods)
	}
}

func TestReconcileResumesFromCurrentStateAfterRevisionRolloutRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &startedAt,
		TargetRevision: "nifi-new",
		CompletedPods:  []string{"nifi-2"},
	}
	cluster.Status.LastOperation = runningOperation("Rollout", "Deleted pod nifi-2 to advance StatefulSet revision \"nifi-new\"")

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.UpdatedReplicas = 1
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted on resume")
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected nifi-2 to remain present: %v", err)
	}
}

func TestReconcileAdvancesRevisionRolloutToNextOrdinalAfterHealthyReplacement(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &startedAt,
		TargetRevision: "nifi-new",
		CompletedPods:  []string{"nifi-2"},
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-newer")
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.UpdatedReplicas = 1
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after advancing revision rollout, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted after nifi-2 replacement proved healthy")
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected nifi-2 to remain present: %v", err)
	}
}

func TestReconcileSkipsStaleNodeOperationAfterReplacementPodReusesName(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &startedAt,
		TargetRevision: "nifi-new",
		CompletedPods:  []string{"nifi-2"},
	}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		PodUID:    "stale-uid",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-newer")
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.UpdatedReplicas = 1
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after advancing revision rollout, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted after stale node operation was discarded")
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected replacement nifi-2 to remain present: %v", err)
	}
}

func TestReconcileWaitsForReplacementPodWhenCurrentRolloutPodIsTerminating(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &startedAt,
		TargetRevision: "nifi-new",
	}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		PodUID:    "nifi-2-uid",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	terminatingPod := readyPod("nifi-2", "nifi", "nifi-old")
	now := metav1.Now()
	terminatingPod.Finalizers = []string{"kubernetes.io/pod-protection"}
	terminatingPod.DeletionTimestamp = &now

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		terminatingPod,
	}

	nodeManager := &fakeNodeManager{readyImmediately: true}
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while current rollout pod is terminating, got %s", result.RequeueAfter)
	}
	if nodeManager.calls != 0 {
		t.Fatalf("expected no node manager call while the rollout pod is still terminating, got %d", nodeManager.calls)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "WaitingForReplacementPod" {
		t.Fatalf("expected WaitingForReplacementPod reason, got %#v", progressing)
	}
}

func TestReconcileCompletesRevisionRolloutWhenAllPodsMatchTargetRevisionAndHealthIsStable(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &startedAt,
		TargetRevision: "nifi-new",
		CompletedPods:  []string{"nifi-2", "nifi-1", "nifi-0"},
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-newer")
	statefulSet.Status.CurrentReplicas = 1
	statefulSet.Status.UpdatedReplicas = 1
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-new"),
		readyPod("nifi-1", "nifi", "nifi-new"),
		readyPod("nifi-2", "nifi", "nifi-new"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected revision rollout completion without requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.ObservedStatefulSetRevision != "nifi-new" {
		t.Fatalf("expected observed revision to advance to pinned target revision, got %q", updatedCluster.Status.ObservedStatefulSetRevision)
	}
	if updatedCluster.Status.Rollout.Trigger != "" {
		t.Fatalf("expected rollout status to clear after completion, got %+v", updatedCluster.Status.Rollout)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected successful last operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
}

func TestReconcileCompletesManagedRevisionRolloutAcrossAllOrdinals(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{nil, nil, nil},
		healthResponses: []healthResponse{
			{result: healthyResult(3)},
			{result: healthyResult(3)},
			{result: healthyResult(3)},
			{result: healthyResult(3)},
		},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}

	assertDeleted := func(podName string) {
		t.Helper()
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, &corev1.Pod{}); err == nil {
			t.Fatalf("expected %s to be deleted", podName)
		}
	}

	createReplacement := func(name string, createdAt time.Time) {
		t.Helper()
		replacement := readyPodAt(name, "nifi", "nifi-new", createdAt)
		replacement.UID = types.UID(name + "-replacement-" + createdAt.Format("150405"))
		if err := k8sClient.Create(ctx, &replacement); err != nil {
			t.Fatalf("create replacement pod %s: %v", name, err)
		}
	}

	updateStatefulSetStatus := func(currentReplicas, updatedReplicas int32) {
		t.Helper()
		current := &appsv1.StatefulSet{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), current); err != nil {
			t.Fatalf("get statefulset: %v", err)
		}
		current.Status.CurrentRevision = "nifi-old"
		current.Status.UpdateRevision = "nifi-new"
		current.Status.CurrentReplicas = currentReplicas
		current.Status.UpdatedReplicas = updatedReplicas
		current.Status.ReadyReplicas = replicas
		if err := k8sClient.Update(ctx, current); err != nil {
			t.Fatalf("update statefulset status: %v", err)
		}
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	assertDeleted("nifi-2")
	createReplacement("nifi-2", time.Now().Add(time.Minute))
	updateStatefulSetStatus(2, 1)

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}
	assertDeleted("nifi-1")
	createReplacement("nifi-1", time.Now().Add(2*time.Minute))
	updateStatefulSetStatus(1, 2)

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("third reconcile returned error: %v", err)
	}
	assertDeleted("nifi-0")
	createReplacement("nifi-0", time.Now().Add(3*time.Minute))
	updateStatefulSetStatus(0, 3)

	result, err := reconciler.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("fourth reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected rollout completion without requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.ObservedStatefulSetRevision != "nifi-new" {
		t.Fatalf("expected observed revision to advance to nifi-new, got %q", updatedCluster.Status.ObservedStatefulSetRevision)
	}
	if updatedCluster.Status.Rollout.Trigger != "" {
		t.Fatalf("expected rollout status to clear after full rollout, got %+v", updatedCluster.Status.Rollout)
	}
}

func TestReconcileTriggersRolloutForConfigMapDrift(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=bar"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after config rollout deletion, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted for config drift")
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Rollout.Trigger != platformv1alpha1.RolloutTriggerConfigDrift {
		t.Fatalf("expected config drift rollout trigger, got %q", updatedCluster.Status.Rollout.Trigger)
	}
	if updatedCluster.Status.Rollout.TargetConfigHash == "" {
		t.Fatalf("expected target config hash to be captured")
	}
	condition := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if condition == nil || condition.Reason != "PodDeleted" {
		t.Fatalf("expected progressing condition to reflect pod deletion, got %#v", condition)
	}
}

func TestReconcileTriggersRolloutForNonTLSSecretDrift(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-auth"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	authSecret := watchedSecret("nifi-auth", corev1.SecretTypeOpaque, map[string][]byte{
		"username": []byte("admin"),
		"password": []byte("new-password"),
	})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, authSecret, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted for non-TLS secret drift")
	}
}

func TestReconcileStartsTLSAutoreloadObservationForStableContentDrift(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	currentTLSConfigHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	cluster.Status.ObservedTLSConfigurationHash = currentTLSConfigHash

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while observing TLS drift, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.TLS.ObservationStartedAt == nil {
		t.Fatalf("expected TLS observation to start")
	}
	if updatedCluster.Status.Rollout.Trigger != "" {
		t.Fatalf("expected no rollout trigger while observing TLS drift, got %q", updatedCluster.Status.Rollout.Trigger)
	}
	if updatedCluster.Status.ObservedCertificateHash != "old-certificate-hash" {
		t.Fatalf("expected observed certificate hash to stay unchanged during observation")
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "TLSAutoreloadObserving" {
		t.Fatalf("expected TLS autoreload observing reason, got %#v", progressing)
	}
}

func TestReconcileResolvesTLSContentDriftWithoutRolloutAfterObservation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	currentTLSConfigHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})
	currentCertificateHash := aggregateCertificateHash([]corev1.Secret{*tlsSecret})
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = currentTLSConfigHash
	startedAt := metav1.NewTime(time.Now().Add(-2 * tlsAutoreloadObservationWindow).UTC())
	cluster.Status.TLS = platformv1alpha1.TLSStatus{
		ObservationStartedAt:       &startedAt,
		TargetCertificateHash:      currentCertificateHash,
		TargetTLSConfigurationHash: currentTLSConfigHash,
	}

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses:  []healthResponse{{result: healthyResult(3)}},
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue after TLS observation resolution, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.ObservedCertificateHash != currentCertificateHash {
		t.Fatalf("expected observed certificate hash to advance after successful observation")
	}
	if updatedCluster.Status.TLS.ObservationStartedAt != nil {
		t.Fatalf("expected TLS observation to clear after resolution")
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "TLSDriftResolvedWithoutRestart" {
		t.Fatalf("expected TLS resolved reason, got %#v", progressing)
	}
}

func TestReconcileEscalatesTLSContentDriftWhenHealthDegrades(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	currentTLSConfigHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = currentTLSConfigHash
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses:    []healthResponse{{result: ClusterHealthResult{ExpectedReplicas: 3, ReadyPods: 3, ReachablePods: 3, ConvergedPods: 1}, err: errors.New("cluster health gate not yet satisfied")}},
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected rollout requeue after TLS escalation, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted for TLS rollout")
	}
}

func TestReconcileAlwaysRestartPolicyEscalatesTLSContentDrift(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	cluster.Spec.RestartPolicy.TLSDrift = platformv1alpha1.TLSDiffPolicyAlwaysRestart

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	currentTLSConfigHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = currentTLSConfigHash
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted for always-restart TLS policy")
	}
}

func TestReconcileMaterialTLSConfigChangeTriggersRolloutImmediately(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	cluster.Spec.RestartPolicy.TLSDrift = platformv1alpha1.TLSDiffPolicyObserveOnly

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	cluster.Status.ObservedTLSConfigurationHash = "old-tls-config-hash"
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})
	cluster.Status.ObservedCertificateHash = aggregateCertificateHash([]corev1.Secret{*tlsSecret})
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted for material TLS config change")
	}
}

func TestReconcileResumesTLSObservationAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	currentTLSConfigHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})
	currentCertificateHash := aggregateCertificateHash([]corev1.Secret{*tlsSecret})
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = currentTLSConfigHash
	startedAt := metav1.NewTime(time.Now().Add(-10 * time.Second).UTC())
	cluster.Status.TLS = platformv1alpha1.TLSStatus{
		ObservationStartedAt:       &startedAt,
		TargetCertificateHash:      currentCertificateHash,
		TargetTLSConfigurationHash: currentTLSConfigHash,
	}

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected TLS observation to keep requeueing, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.TLS.ObservationStartedAt == nil {
		t.Fatalf("expected TLS observation startedAt to be preserved")
	}
	if updatedCluster.Status.TLS.ObservationStartedAt.Time.After(startedAt.Time.Add(2 * time.Second)) {
		t.Fatalf("expected TLS observation startedAt to be preserved")
	}
}

func TestReconcileResumesTLSRolloutAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = "old-tls-config-hash"
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:                    platformv1alpha1.RolloutTriggerTLSDrift,
		StartedAt:                  &startedAt,
		TargetCertificateHash:      "new-certificate-hash",
		TargetTLSConfigurationHash: "new-tls-config-hash",
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":      []byte("keystore"),
		"truststore":   []byte("truststore"),
		"ca.crt":       []byte("ca"),
		"keystorePass": []byte("changeit"),
	})
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted on TLS rollout resume")
	}
}

func TestReconcileResumesFinalOrdinalTLSRolloutWhenConflictRefreshReportsNotConnected(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = "old-tls-config-hash"
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:                    platformv1alpha1.RolloutTriggerTLSDrift,
		StartedAt:                  &startedAt,
		TargetCertificateHash:      "new-certificate-hash",
		TargetTLSConfigurationHash: "new-tls-config-hash",
		CompletedPods:              []string{"nifi-2", "nifi-1"},
	}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-0",
		PodUID:    string(types.UID("nifi-0-uid")),
		NodeID:    "node-0",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	statefulSet.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{
			Name: "SINGLE_USER_CREDENTIALS_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-auth"},
					Key:                  "username",
				},
			},
		},
		{
			Name: "SINGLE_USER_CREDENTIALS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-auth"},
					Key:                  "password",
				},
			},
		},
	}

	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":            []byte("keystore"),
		"truststore.p12":     []byte("truststore"),
		"ca.crt":             []byte("ca"),
		"keystorePassword":   []byte("changeit"),
		"truststorePassword": []byte("changeit"),
		"sensitivePropsKey":  []byte("sensitive"),
	})
	authSecret := watchedSecret("nifi-auth", corev1.SecretTypeOpaque, map[string][]byte{
		"username": []byte("admin"),
		"password": []byte("ChangeMeChangeMe1!"),
	})
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(3*time.Minute)),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{fmt.Errorf("global health gate should not run while TLS node preparation is resuming")},
		healthResponses:   []healthResponse{{err: fmt.Errorf("global health gate should not run while TLS node preparation is resuming")}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, tlsSecret, authSecret, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &LiveNodeManager{
		KubeClient: k8sClient,
		NiFiClient: &fakeNiFiClient{
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-0", Address: "nifi-0.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusDisconnected},
				{NodeID: "node-1", Address: "nifi-1.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
				{NodeID: "node-2", Address: "nifi-2.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
			}},
			getNodesErrs: []error{
				nil,
				&nifi.APIError{StatusCode: 409, Message: "Cannot replicate request to Node nifi-0.nifi-headless.nifi.svc.cluster.local:8443 because the node is not connected"},
			},
			updateErr: &nifi.APIError{StatusCode: 409, Message: "node is not connected"},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after deleting final TLS rollout pod, got %s", result.RequeueAfter)
	}
	if healthChecker.podReadyCalls != 0 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected no global health gate calls while TLS node preparation is resuming, got podReady=%d health=%d", healthChecker.podReadyCalls, healthChecker.healthCalls)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-0"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-0 to be deleted after final TLS node preparation advanced")
	}
}

func TestReconcileResumesConfigDriftRolloutAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &startedAt,
		TargetConfigHash: "new-config-hash",
	}
	cluster.Status.LastOperation = runningOperation("Rollout", "Deleted pod nifi-2 to apply watched config drift")

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=baz"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])

	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted on config rollout resume")
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected nifi-2 to remain present: %v", err)
	}
}

func TestReconcileAdvancesConfigDriftRolloutToNextOrdinalAfterHealthyReplacement(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &startedAt,
		TargetConfigHash: "new-config-hash",
		CompletedPods:    []string{"nifi-2"},
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=baz"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after config drift advancement, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted after nifi-2 replacement proved healthy")
	}
}

func TestReconcileCompletesConfigDriftRolloutWhenAllPodsRecreatedAndHealthy(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &startedAt,
		TargetConfigHash: "new-config-hash",
		CompletedPods:    []string{"nifi-2", "nifi-1", "nifi-0"},
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=baz"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected config drift rollout completion without requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Rollout.Trigger != "" {
		t.Fatalf("expected rollout to clear after config drift completion, got %+v", updatedCluster.Status.Rollout)
	}
	if updatedCluster.Status.ObservedConfigHash != "new-config-hash" {
		t.Fatalf("expected observed config hash to advance, got %q", updatedCluster.Status.ObservedConfigHash)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected successful last operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
}

func TestReconcileClearsStaleConfigDriftNodeOperationWhenReplacementUsesSameName(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	startedAt := metav1.NewTime(time.Now().Add(-5 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &startedAt,
		TargetConfigHash: "new-config-hash",
		CompletedPods:    []string{"nifi-2"},
	}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		PodUID:    "stale-uid",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(2*time.Minute)),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=baz"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after clearing stale config drift node operation, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-1"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-1 to be deleted after stale config drift node operation was discarded")
	}
}

func TestReconcileResumesConfigDriftNodePreparationAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &startedAt,
		TargetConfigHash: "new-config-hash",
	}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		PodUID:    "nifi-2-uid",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(-2*time.Minute)),
	}
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=baz"})

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, configMap, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for restart"}},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after resumed config drift deletion, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted after resumed config drift node preparation")
	}
}

func TestReconcileHibernatesManagedClusterAndCapturesLastRunningReplicas(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	pvc := repositoryPVC("nifi-content-nifi-0")

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, pvc, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while hibernating, got %s", result.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected target replicas to scale down by one step to 2, got %d", got)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.LastRunningReplicas != replicas {
		t.Fatalf("expected lastRunningReplicas=%d, got %d", replicas, updatedCluster.Status.Hibernation.LastRunningReplicas)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseRunning {
		t.Fatalf("expected running last operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}

	persistedPVC := &corev1.PersistentVolumeClaim{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), persistedPVC); err != nil {
		t.Fatalf("expected PVC to remain present after hibernation scale-down: %v", err)
	}
}

func TestReconcileMarksClusterHibernatedAfterScaleDownCompletes(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.LastRunningReplicas = 3

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue after hibernation completion, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	hibernated := updatedCluster.GetCondition(platformv1alpha1.ConditionHibernated)
	if hibernated == nil || hibernated.Status != metav1.ConditionTrue {
		t.Fatalf("expected Hibernated condition true, got %#v", hibernated)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected succeeded last operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
}

func TestReconcileRestoresPriorReplicaCount(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.Hibernation.LastRunningReplicas = 3
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   platformv1alpha1.ConditionHibernated,
		Status: metav1.ConditionTrue,
		Reason: "Hibernated",
	}}

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet)

	firstResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if firstResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after scaling up restore, got %s", firstResult.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 3 {
		t.Fatalf("expected restore target replicas=3, got %d", got)
	}

	updatedStatefulSet.Status.ReadyReplicas = 3
	updatedStatefulSet.Status.CurrentReplicas = 3
	updatedStatefulSet.Status.UpdatedReplicas = 3
	if err := k8sClient.Status().Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset status: %v", err)
	}

	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}
	for i := range pods {
		if err := k8sClient.Create(ctx, &pods[i]); err != nil {
			t.Fatalf("create restored pod %s: %v", pods[i].Name, err)
		}
	}

	secondResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}
	if secondResult.RequeueAfter != 0 {
		t.Fatalf("expected restore completion without requeue, got %s", secondResult.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected succeeded restore operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
	hibernated := updatedCluster.GetCondition(platformv1alpha1.ConditionHibernated)
	if hibernated == nil || hibernated.Status != metav1.ConditionFalse || hibernated.Reason != "Running" {
		t.Fatalf("expected Hibernated condition false/running after restore, got %#v", hibernated)
	}
}

func TestReconcileRestoreUsesBaselineReplicasWhenLastRunningReplicaIsMissing(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.Hibernation.BaselineReplicas = 3
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   platformv1alpha1.ConditionHibernated,
		Status: metav1.ConditionTrue,
		Reason: "Hibernated",
	}}

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after baseline restore scale-up, got %s", result.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 3 {
		t.Fatalf("expected baseline restore replicas=3, got %d", got)
	}
}

func TestReconcileRestoreFallsBackToSingleReplicaWhenNoRestoreHintsExist(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   platformv1alpha1.ConditionHibernated,
		Status: metav1.ConditionTrue,
		Reason: "Hibernated",
	}}

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after fallback restore scale-up, got %s", result.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != hibernationFallbackReplicas {
		t.Fatalf("expected fallback restore replicas=%d, got %d", hibernationFallbackReplicas, got)
	}
}

func TestReconcileRecordsBaselineReplicasWhileRunning(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected steady state without requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.BaselineReplicas != replicas {
		t.Fatalf("expected baselineReplicas=%d, got %d", replicas, updatedCluster.Status.Hibernation.BaselineReplicas)
	}
	if healthChecker.healthCalls != 1 || healthChecker.checkCalls != 0 {
		t.Fatalf("expected disabled steady state to use WaitForClusterHealthy only, got wait=%d check=%d", healthChecker.healthCalls, healthChecker.checkCalls)
	}
}

func TestReconcileDoesNotShrinkBaselineReplicasDuringPartialRestore(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.Hibernation.BaselineReplicas = 3

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		healthResponses: []healthResponse{{result: healthyResult(2)}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected steady state without requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.BaselineReplicas != 3 {
		t.Fatalf("expected baselineReplicas to remain 3, got %d", updatedCluster.Status.Hibernation.BaselineReplicas)
	}
	if healthChecker.healthCalls != 1 || healthChecker.checkCalls != 0 {
		t.Fatalf("expected disabled steady state to use WaitForClusterHealthy only, got wait=%d check=%d", healthChecker.healthCalls, healthChecker.checkCalls)
	}
}

func TestReconcileKeepsPollingSteadyStateWhenAutoscalingEnabled(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 3,
		MaxReplicas: 3,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResultWithPods("nifi", 3)}},
	}
	reconciler, _ := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected steady state autoscaling poll requeue, got %s", result.RequeueAfter)
	}
	if healthChecker.checkCalls != 1 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected autoscaling steady state to use one-shot health sampling only, got wait=%d check=%d", healthChecker.healthCalls, healthChecker.checkCalls)
	}
}

func TestReconcileSettledAutoscalingScaleDownStillAppliesLaterScaleUpIntent(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeEnforced,
		MinReplicas: 3,
		MaxReplicas: 3,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: false,
		},
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.LastOperation = succeededOperation("AutoscalingScaleDown", "Managed autoscaling safely settled at 2 replicas")
	cluster.Status.Autoscaling.LastScaleDownTime = &metav1.Time{Time: time.Date(2026, 3, 13, 10, 0, 0, 0, time.UTC)}

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		checkResponses: []healthResponse{{
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
			err: errors.New("strict health gate still sees a disconnected former node"),
		}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected scale-up after settled scale-down to requeue, got %s", result.RequeueAfter)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated StatefulSet: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 3 {
		t.Fatalf("expected StatefulSet to scale back to 3 replicas, got %d", got)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.Phase != platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle {
		t.Fatalf("expected autoscaling execution phase %q, got %q", platformv1alpha1.AutoscalingExecutionPhaseScaleUpSettle, updatedCluster.Status.Autoscaling.Execution.Phase)
	}
	if updatedCluster.Status.Autoscaling.Execution.State != platformv1alpha1.AutoscalingExecutionStateRunning {
		t.Fatalf("expected autoscaling execution state %q, got %q", platformv1alpha1.AutoscalingExecutionStateRunning, updatedCluster.Status.Autoscaling.Execution.State)
	}
	if !strings.HasPrefix(updatedCluster.Status.Autoscaling.LastScalingDecision, "ScaleUp: increased target StatefulSet replicas from 2 to 3") {
		t.Fatalf("expected scale-up decision to be recorded, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}
	if healthChecker.checkCalls == 0 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected settled autoscaling handoff to use one-shot health sampling only, got wait=%d check=%d", healthChecker.healthCalls, healthChecker.checkCalls)
	}
}

func TestReconcileAutoscalingSteadyStateKeepsAvailableWhenFormerNodesRemain(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeAdvisory,
		MinReplicas: 2,
		MaxReplicas: 2,
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.Replicas.Desired = 2

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		checkResponses: []healthResponse{{
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
			err: errors.New("strict health gate still sees disconnected former nodes"),
		}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected steady state autoscaling poll requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	available := updatedCluster.GetCondition(platformv1alpha1.ConditionAvailable)
	if available == nil || available.Status != metav1.ConditionTrue {
		t.Fatalf("expected cluster to remain available while former nodes drain out, got %#v", available)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Status != metav1.ConditionFalse {
		t.Fatalf("expected cluster to remain non-progressing while former nodes drain out, got %#v", progressing)
	}
	if updatedCluster.Status.Autoscaling.Reason == autoscalingReasonProgressing {
		t.Fatalf("expected autoscaling to stay out of progressing status when only former nodes remain, got %#v", updatedCluster.Status.Autoscaling)
	}
	if healthChecker.checkCalls != 1 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected autoscaling steady state to use one-shot health sampling only, got wait=%d check=%d", healthChecker.healthCalls, healthChecker.checkCalls)
	}
}

func TestReconcilePausesAutoscalingScaleDownForHibernation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateRunning,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-45 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		Message:        "Waiting for the autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScalingDecision = "ScaleDown: reduced target StatefulSet replicas from 3 to 2 after preparing pod nifi-2"
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down settlement")
	cluster.Status.Hibernation.LastRunningReplicas = 3

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	statefulSet.Status.Replicas = 2
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 2
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0], &pods[1])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected hibernation precedence to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.State != platformv1alpha1.AutoscalingExecutionStateBlocked {
		t.Fatalf("expected autoscaling execution to be blocked during hibernation precedence, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownPausedForHibernation {
		t.Fatalf("expected hibernation pause reason, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if !strings.Contains(updatedCluster.Status.Autoscaling.LastScalingDecision, "paused while hibernation has precedence") {
		t.Fatalf("expected autoscaling pause decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason == "AutoscalingScaleDown" {
		t.Fatalf("expected higher-precedence hibernation progressing condition, got %#v", progressing)
	}
}

func TestReconcilePausesAutoscalingScaleDownForRestore(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateRunning,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-45 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		Message:        "Waiting for the autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScalingDecision = "ScaleDown: reduced target StatefulSet replicas from 3 to 2 after preparing pod nifi-2"
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down settlement")
	cluster.Status.Hibernation.LastRunningReplicas = 3
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Restoring",
		Message:            "Restore is scaling the StatefulSet back up",
		LastTransitionTime: metav1.Now(),
	})

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")
	statefulSet.Status.Replicas = 0
	statefulSet.Status.CurrentReplicas = 0
	statefulSet.Status.ReadyReplicas = 0
	statefulSet.Status.UpdatedReplicas = 0

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected restore precedence to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownPausedForRestore {
		t.Fatalf("expected restore pause reason, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if !strings.Contains(updatedCluster.Status.Autoscaling.LastScalingDecision, "paused while restore has precedence") {
		t.Fatalf("expected restore pause decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}
}

func TestReconcilePausesAutoscalingScaleDownForRolloutAndResumesAfterConflictClears(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateRunning,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-45 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		Message:        "Waiting for the autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScalingDecision = "ScaleDown: reduced target StatefulSet replicas from 3 to 2 after preparing pod nifi-2"
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleDown", "Waiting for autoscaling scale-down settlement")

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	statefulSet.Status.Replicas = 2
	statefulSet.Status.CurrentReplicas = 2
	statefulSet.Status.ReadyReplicas = 2
	statefulSet.Status.UpdatedReplicas = 0
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(2)}},
	}, cluster, statefulSet, &pods[0], &pods[1])

	firstResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if firstResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected rollout precedence to requeue, got %s", firstResult.RequeueAfter)
	}

	pausedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), pausedCluster); err != nil {
		t.Fatalf("get paused cluster: %v", err)
	}
	if pausedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownPausedForRollout {
		t.Fatalf("expected rollout pause reason, got %#v", pausedCluster.Status.Autoscaling.Execution)
	}
	if !strings.Contains(pausedCluster.Status.Autoscaling.LastScalingDecision, "paused while higher-precedence rollout work is active") {
		t.Fatalf("expected rollout pause decision, got %q", pausedCluster.Status.Autoscaling.LastScalingDecision)
	}

	currentStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), currentStatefulSet); err != nil {
		t.Fatalf("get statefulset after paused reconcile: %v", err)
	}
	currentStatefulSet.Status.CurrentRevision = "nifi-new"
	currentStatefulSet.Status.UpdateRevision = "nifi-new"
	currentStatefulSet.Status.Replicas = 2
	currentStatefulSet.Status.CurrentReplicas = 2
	currentStatefulSet.Status.ReadyReplicas = 2
	currentStatefulSet.Status.UpdatedReplicas = 2
	if err := k8sClient.Status().Update(ctx, currentStatefulSet); err != nil {
		t.Fatalf("update statefulset status after rollout clear: %v", err)
	}
	for _, podName := range []string{"nifi-0", "nifi-1"} {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, pod); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get pod %s after rollout clear: %v", podName, err)
			}
			replacement := readyPod(podName, "nifi", "nifi-new")
			if err := k8sClient.Create(ctx, &replacement); err != nil {
				t.Fatalf("create replacement pod %s after rollout clear: %v", podName, err)
			}
			continue
		}
		if pod.Labels == nil {
			pod.Labels = map[string]string{}
		}
		pod.Labels[controllerRevisionHashLabel] = "nifi-new"
		if err := k8sClient.Update(ctx, pod); err != nil {
			t.Fatalf("update pod %s revision after rollout clear: %v", podName, err)
		}
	}

	pausedCluster.Status.ObservedStatefulSetRevision = "nifi-new"
	pausedCluster.Status.Rollout = platformv1alpha1.RolloutStatus{}
	pausedCluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "NoDrift",
		Message:            "No rollout is currently in progress",
		LastTransitionTime: metav1.Now(),
	})
	if err := k8sClient.Status().Update(ctx, pausedCluster); err != nil {
		t.Fatalf("update cluster status after rollout clear: %v", err)
	}

	secondResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}
	if secondResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected resumed autoscaling settle to requeue, got %s", secondResult.RequeueAfter)
	}

	resumedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), resumedCluster); err != nil {
		t.Fatalf("get resumed cluster: %v", err)
	}
	if resumedCluster.Status.Autoscaling.Execution.Phase != "" {
		t.Fatalf("expected autoscaling execution to clear after conflict-free settle, got %#v", resumedCluster.Status.Autoscaling.Execution)
	}
	if resumedCluster.Status.LastOperation.Type != "AutoscalingScaleDown" || resumedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseSucceeded {
		t.Fatalf("expected autoscaling settle to resume and complete, got %#v", resumedCluster.Status.LastOperation)
	}
}

func TestReconcilePausesAutoscalingScaleDownForTLSRollout(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode: platformv1alpha1.AutoscalingModeEnforced,
		ScaleDown: platformv1alpha1.AutoscalingScaleDownPolicy{
			Enabled: true,
		},
		MinReplicas: 1,
		MaxReplicas: 4,
	}
	cluster.Spec.RestartPolicy.TLSDrift = platformv1alpha1.TLSDiffPolicyObserveOnly
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	cluster.Status.Autoscaling.Execution = platformv1alpha1.AutoscalingExecutionStatus{
		Phase:          platformv1alpha1.AutoscalingExecutionPhaseScaleDownSettle,
		State:          platformv1alpha1.AutoscalingExecutionStateRunning,
		StartedAt:      &metav1.Time{Time: time.Now().UTC().Add(-45 * time.Second)},
		TargetReplicas: ptrTo(int32(2)),
		Message:        "Waiting for the autoscaling scale-down step to settle at 2 replicas",
	}
	cluster.Status.Autoscaling.LastScalingDecision = "ScaleDown: reduced target StatefulSet replicas from 3 to 2 after preparing pod nifi-2"
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"
	cluster.Status.ObservedTLSConfigurationHash = "old-tls-config-hash"

	statefulSet := managedStatefulSet("nifi", 2, "nifi-rev", "nifi-rev")
	currentTLSHash, err := computeTLSConfigurationHash(statefulSet)
	if err != nil {
		t.Fatalf("compute tls config hash: %v", err)
	}
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"tls.p12":            []byte("rotated-keystore"),
		"truststore.p12":     []byte("truststore"),
		"keystorePassword":   []byte("changeit"),
		"truststorePassword": []byte("changeit"),
	})
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		checkResponses: []healthResponse{{result: healthyResultWithPods("nifi", 2)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected TLS observation precedence to requeue, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Autoscaling.Execution.BlockedReason != autoscalingBlockedReasonScaleDownPausedForRollout {
		t.Fatalf("expected TLS pause reason, got %#v", updatedCluster.Status.Autoscaling.Execution)
	}
	if !strings.Contains(updatedCluster.Status.Autoscaling.LastScalingDecision, "TLS drift rollout pending") {
		t.Fatalf("expected TLS pause decision, got %q", updatedCluster.Status.Autoscaling.LastScalingDecision)
	}
	if updatedCluster.Status.Rollout.Trigger != platformv1alpha1.RolloutTriggerTLSDrift {
		t.Fatalf("expected TLS rollout trigger, got %#v", updatedCluster.Status.Rollout)
	}
	if updatedCluster.Status.Rollout.TargetTLSConfigurationHash != currentTLSHash {
		t.Fatalf("expected TLS rollout target hash %q, got %#v", currentTLSHash, updatedCluster.Status.Rollout)
	}
}

func TestReconcileResumesSafelyDuringHibernation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.LastRunningReplicas = 3
	cluster.Status.LastOperation = runningOperation("Hibernation", "Scaled StatefulSet \"nifi\" to 0 replicas; waiting for pods to terminate")

	statefulSet := managedStatefulSet("nifi", 0, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{}, cluster, statefulSet, &pods[0])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while waiting for remaining pods to terminate, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.LastRunningReplicas != 3 {
		t.Fatalf("expected lastRunningReplicas to be preserved, got %d", updatedCluster.Status.Hibernation.LastRunningReplicas)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "WaitingForScaleDown" {
		t.Fatalf("expected WaitingForScaleDown progressing reason, got %#v", progressing)
	}
}

func TestReconcileDoesNotShrinkLastRunningReplicasDuringHibernation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.LastRunningReplicas = 3

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(2)}},
	}, cluster, statefulSet, &pods[0], &pods[1])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{{
			result: NodePreparationResult{Ready: true, Message: "NiFi node node-1 is OFFLOADED and ready for hibernation"},
		}},
	}
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after hibernation step, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.LastRunningReplicas != 3 {
		t.Fatalf("expected lastRunningReplicas to remain 3, got %d", updatedCluster.Status.Hibernation.LastRunningReplicas)
	}
}

func TestReconcileRecoversRestoreTargetFromBaselineDuringMidHibernation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.BaselineReplicas = 3
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   platformv1alpha1.ConditionHibernated,
		Status: metav1.ConditionFalse,
		Reason: "Hibernating",
	}}

	replicas := int32(2)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(2)}},
	}, cluster, statefulSet, &pods[0], &pods[1])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{{
			result: NodePreparationResult{Ready: true, Message: "NiFi node node-1 is OFFLOADED and ready for hibernation"},
		}},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after hibernation step, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.Hibernation.LastRunningReplicas != 3 {
		t.Fatalf("expected lastRunningReplicas to recover from baseline=3, got %d", updatedCluster.Status.Hibernation.LastRunningReplicas)
	}
}

func TestReconcileHibernationScalesFinalReplicaToZeroWithoutNodePreparation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.LastRunningReplicas = 3

	replicas := int32(1)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(1)}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{{
			err: fmt.Errorf("final hibernation step should not invoke node preparation"),
		}},
	}
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after final hibernation scale-down, got %s", result.RequeueAfter)
	}
	if nodeManager.calls != 0 {
		t.Fatalf("expected no node preparation calls for the final hibernation step, got %d", nodeManager.calls)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 0 {
		t.Fatalf("expected final hibernation step to scale to 0 replicas, got %d", got)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.NodeOperation.PodName != "" {
		t.Fatalf("expected node operation to be cleared after final hibernation scale-down, got %#v", updatedCluster.Status.NodeOperation)
	}
	if updatedCluster.Status.Replicas.Desired != 0 {
		t.Fatalf("expected desired replicas to be recorded as 0, got %d", updatedCluster.Status.Replicas.Desired)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "ScalingDown" {
		t.Fatalf("expected ScalingDown progressing reason, got %#v", progressing)
	}
}

func TestReconcileRestoreWaitsForStableHealthBeforeSuccess(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.Hibernation.LastRunningReplicas = 3
	cluster.Status.LastOperation = runningOperation("Hibernation", "Waiting for pod readiness and cluster convergence while restoring to 3 replicas")
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   platformv1alpha1.ConditionHibernated,
		Status: metav1.ConditionFalse,
		Reason: "Restoring",
	}}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses: []healthResponse{{
			result: ClusterHealthResult{ExpectedReplicas: 3, ReadyPods: 3, ReachablePods: 3, ConvergedPods: 1},
			err:    errors.New("cluster health gate not yet satisfied"),
		}},
	}, cluster, statefulSet)

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while waiting for restore health, got %s", result.RequeueAfter)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseRunning {
		t.Fatalf("expected running restore operation, got %s", updatedCluster.Status.LastOperation.Phase)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "WaitingForClusterHealth" {
		t.Fatalf("expected WaitingForClusterHealth progressing reason, got %#v", progressing)
	}
}

func TestReconcileWaitsForNiFiPreparationBeforeDeletingRolloutPod(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil, nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}, {result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{
			{
				result: NodePreparationResult{
					RequeueNow: true,
					Message:    "Requested NiFi disconnect for node node-2",
					Operation: platformv1alpha1.NodeOperationStatus{
						Purpose: platformv1alpha1.NodeOperationPurposeRestart,
						PodName: "nifi-2",
						NodeID:  "node-2",
						Stage:   platformv1alpha1.NodeOperationStageDisconnecting,
						StartedAt: func() *metav1.Time {
							now := metav1.Now()
							return &now
						}(),
					},
				},
			},
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for restart"}},
		},
	}
	reconciler.NodeManager = nodeManager

	firstResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if firstResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected polling requeue while preparing node, got %+v", firstResult)
	}
	for _, podName := range []string{"nifi-0", "nifi-1", "nifi-2"} {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, pod); err != nil {
			t.Fatalf("expected pod %s to remain present while NiFi preparation is in progress: %v", podName, err)
		}
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster after first reconcile: %v", err)
	}
	if updatedCluster.Status.NodeOperation.PodName != "nifi-2" {
		t.Fatalf("expected node operation to persist target pod, got %+v", updatedCluster.Status.NodeOperation)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "PreparingNodeForRestart" {
		t.Fatalf("expected PreparingNodeForRestart reason, got %#v", progressing)
	}

	secondResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}
	if secondResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after deleting prepared pod, got %s", secondResult.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted only after NiFi preparation completed")
	}
}

func TestReconcileHibernationScalesDownHighestOrdinalOneStepAtATime(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{nil, nil, nil},
		checkResponses: []healthResponse{
			{
				result: ClusterHealthResult{
					ExpectedReplicas: 3,
					ReadyPods:        3,
					ReachablePods:    3,
					ConvergedPods:    3,
					Pods: []PodHealth{
						{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 3, TotalNodeCount: 3},
						{PodName: "nifi-1", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 3, TotalNodeCount: 3},
						{PodName: "nifi-2", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 3, TotalNodeCount: 3},
					},
				},
			},
			{
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
				err: errors.New("strict health gate still sees a disconnected former node"),
			},
			{
				result: ClusterHealthResult{
					ExpectedReplicas: 1,
					ReadyPods:        1,
					ReachablePods:    1,
					ConvergedPods:    0,
					Pods: []PodHealth{
						{PodName: "nifi-0", Ready: true, APIReachable: true, Clustered: true, ConnectedToCluster: true, ConnectedNodeCount: 1, TotalNodeCount: 3},
					},
				},
				err: errors.New("strict health gate still sees disconnected former nodes"),
			},
		},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for hibernation"}},
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-1 is OFFLOADED and ready for hibernation"}},
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-0 is OFFLOADED and ready for hibernation"}},
		},
	}
	reconciler.NodeManager = nodeManager

	firstResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("first reconcile returned error: %v", err)
	}
	if firstResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after first hibernation step, got %s", firstResult.RequeueAfter)
	}
	if len(nodeManager.pods) != 1 || nodeManager.pods[0] != "nifi-2" {
		t.Fatalf("expected first hibernation target to be highest ordinal pod nifi-2, got %v", nodeManager.pods)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset after first hibernation step: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected hibernation to reduce replicas to 2 after first step, got %d", got)
	}

	if err := k8sClient.Delete(ctx, &pods[2]); err != nil {
		t.Fatalf("delete highest ordinal pod to simulate settled scale-down: %v", err)
	}
	updatedStatefulSet.Spec.Replicas = int32ptr(2)
	updatedStatefulSet.Status.CurrentReplicas = 2
	updatedStatefulSet.Status.ReadyReplicas = 2
	updatedStatefulSet.Status.UpdatedReplicas = 2
	if err := k8sClient.Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset spec after first scale-down: %v", err)
	}
	if err := k8sClient.Status().Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset status after first scale-down: %v", err)
	}

	secondResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("second reconcile returned error: %v", err)
	}
	if secondResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after second hibernation step, got %s", secondResult.RequeueAfter)
	}
	if len(nodeManager.pods) != 2 || nodeManager.pods[1] != "nifi-1" {
		t.Fatalf("expected second hibernation target to be next highest ordinal pod nifi-1, got %v", nodeManager.pods)
	}

	if err := k8sClient.Delete(ctx, &pods[1]); err != nil {
		t.Fatalf("delete next highest ordinal pod to simulate settled scale-down: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset before second scale-down settle: %v", err)
	}
	updatedStatefulSet.Spec.Replicas = int32ptr(1)
	updatedStatefulSet.Status.CurrentReplicas = 1
	updatedStatefulSet.Status.ReadyReplicas = 1
	updatedStatefulSet.Status.UpdatedReplicas = 1
	if err := k8sClient.Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset spec after second scale-down: %v", err)
	}
	if err := k8sClient.Status().Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset status after second scale-down: %v", err)
	}

	thirdResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("third reconcile returned error: %v", err)
	}
	if thirdResult.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after third hibernation step, got %s", thirdResult.RequeueAfter)
	}
	if len(nodeManager.pods) != 2 {
		t.Fatalf("expected final hibernation step to skip explicit node preparation, got %v", nodeManager.pods)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset after final scale-down request: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 0 {
		t.Fatalf("expected final hibernation step to scale StatefulSet to 0 replicas, got %d", got)
	}

	if err := k8sClient.Delete(ctx, &pods[0]); err != nil {
		t.Fatalf("delete final pod to simulate settled scale-down: %v", err)
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset before final scale-down settle: %v", err)
	}
	updatedStatefulSet.Spec.Replicas = int32ptr(0)
	updatedStatefulSet.Status.CurrentReplicas = 0
	updatedStatefulSet.Status.ReadyReplicas = 0
	updatedStatefulSet.Status.UpdatedReplicas = 0
	if err := k8sClient.Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset spec after third scale-down: %v", err)
	}
	if err := k8sClient.Status().Update(ctx, updatedStatefulSet); err != nil {
		t.Fatalf("update statefulset status after third scale-down: %v", err)
	}

	finalResult, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("final reconcile returned error: %v", err)
	}
	if finalResult.RequeueAfter != 0 {
		t.Fatalf("expected hibernation completion without requeue, got %s", finalResult.RequeueAfter)
	}
	if got := healthChecker.checkReplicas; !reflect.DeepEqual(got, []int32{3, 3, 3, 2, 2, 2, 1, 1, 1}) {
		t.Fatalf("expected hibernation health gate replica targets [3 3 3 2 2 2 1 1 1], got %v", got)
	}
	if got := healthChecker.podReadyReplicas; !reflect.DeepEqual(got, []int32{3, 2, 1}) {
		t.Fatalf("expected pod-ready replica targets [3 2 1], got %v", got)
	}
}

func TestReconcileBlocksRolloutWhenNodePreparationTimesOut(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	startedAt := metav1.NewTime(time.Now().Add(-10 * time.Minute).UTC())
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{{
			result: NodePreparationResult{
				TimedOut: true,
				Message:  "Waiting for NiFi node node-2 to reach OFFLOADED before proceeding",
				Operation: platformv1alpha1.NodeOperationStatus{
					Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
					PodName:   "nifi-2",
					NodeID:    "node-2",
					Stage:     platformv1alpha1.NodeOperationStageOffloading,
					StartedAt: &startedAt,
				},
			},
		}},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue while node preparation is timed out, got %s", result.RequeueAfter)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err != nil {
		t.Fatalf("expected pod to remain present when node preparation timed out: %v", err)
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.NodeOperation.Stage != platformv1alpha1.NodeOperationStageOffloading {
		t.Fatalf("expected node operation stage to remain persisted, got %+v", updatedCluster.Status.NodeOperation)
	}
	degraded := updatedCluster.GetCondition(platformv1alpha1.ConditionDegraded)
	if degraded == nil || degraded.Reason != "NodePreparationTimedOut" || degraded.Status != metav1.ConditionTrue {
		t.Fatalf("expected degraded timeout condition, got %#v", degraded)
	}
}

func TestReconcilePersistsRolloutFailureStatusAndAutoscalingWhenPodDeleteFails(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.Autoscaling = platformv1alpha1.AutoscalingPolicy{
		Mode:        platformv1alpha1.AutoscalingModeEnforced,
		MinReplicas: 4,
		MaxReplicas: 4,
		ScaleUp: platformv1alpha1.AutoscalingScaleUpPolicy{
			Enabled: true,
		},
		Signals: []platformv1alpha1.AutoscalingSignal{
			platformv1alpha1.AutoscalingSignalQueuePressure,
		},
	}
	cluster.Status.Replicas.Desired = 3
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
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
	reconciler.Client = &deleteFailingClient{
		Client:  k8sClient,
		podName: "nifi-2",
		err: apierrors.NewForbidden(
			schema.GroupResource{Resource: "pods"},
			"nifi-2",
			errors.New("delete forbidden"),
		),
	}
	reconciler.APIReader = k8sClient

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}); err == nil {
		t.Fatalf("expected reconcile to return the rollout delete error")
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}

	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "RolloutFailed" || progressing.Status != metav1.ConditionFalse {
		t.Fatalf("expected rollout-failed progressing condition, got %#v", progressing)
	}
	degraded := updatedCluster.GetCondition(platformv1alpha1.ConditionDegraded)
	if degraded == nil || degraded.Reason != "RolloutFailed" || degraded.Status != metav1.ConditionTrue {
		t.Fatalf("expected rollout-failed degraded condition, got %#v", degraded)
	}
	if updatedCluster.Status.Autoscaling.Reason != autoscalingReasonDegraded {
		t.Fatalf("expected autoscaling degraded reason, got %#v", updatedCluster.Status.Autoscaling)
	}
	if updatedCluster.Status.Autoscaling.RecommendedReplicas != nil {
		t.Fatalf("expected no autoscaling recommendation when degraded, got %#v", updatedCluster.Status.Autoscaling)
	}
	if !strings.Contains(updatedCluster.Status.Autoscaling.LastScalingDecision, "Autoscaling is blocked while the cluster is degraded") {
		t.Fatalf("expected degraded autoscaling decision, got %#v", updatedCluster.Status.Autoscaling)
	}
	if updatedCluster.Status.LastOperation.Phase != platformv1alpha1.OperationPhaseFailed {
		t.Fatalf("expected failed last operation, got %#v", updatedCluster.Status.LastOperation)
	}
}

func TestReconcileResumesRolloutAfterControllerRestartDuringNodePreparation(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-2 * time.Minute).UTC())
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for restart"}},
		},
	}
	reconciler.NodeManager = nodeManager

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after deleting resumed pod, got %s", result.RequeueAfter)
	}
	if len(nodeManager.currentStates) != 1 || nodeManager.currentStates[0].Stage != platformv1alpha1.NodeOperationStageOffloading {
		t.Fatalf("expected persisted node operation to be passed back to node manager, got %v", nodeManager.currentStates)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted after resumed node preparation")
	}
}

func TestReconcileResumesRolloutNodePreparationBeforeRecheckingGlobalHealth(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
	startedAt := metav1.NewTime(time.Now().Add(-30 * time.Second).UTC())
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{fmt.Errorf("global health gate should not run while node preparation is resuming")},
		healthResponses:   []healthResponse{{err: fmt.Errorf("global health gate should not run while node preparation is resuming")}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for restart"}},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after deleting resumed pod, got %s", result.RequeueAfter)
	}
	if healthChecker.podReadyCalls != 0 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected no global health gate calls while resuming node preparation, got podReady=%d health=%d", healthChecker.podReadyCalls, healthChecker.healthCalls)
	}
	if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, &corev1.Pod{}); err == nil {
		t.Fatalf("expected nifi-2 to be deleted after resumed node preparation")
	}
}

func TestReconcileResumesHibernationNodePreparationBeforeRecheckingGlobalHealth(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.DesiredState = platformv1alpha1.DesiredStateHibernated
	cluster.Status.Hibernation.LastRunningReplicas = 3
	startedAt := metav1.NewTime(time.Now().Add(-30 * time.Second).UTC())
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeHibernation,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
	statefulSet.Status.CurrentReplicas = 3
	statefulSet.Status.ReadyReplicas = 3
	statefulSet.Status.UpdatedReplicas = 3
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	healthChecker := &fakeHealthChecker{
		podReadyResponses: []error{fmt.Errorf("global health gate should not run while hibernation node preparation is resuming")},
		healthResponses:   []healthResponse{{err: fmt.Errorf("global health gate should not run while hibernation node preparation is resuming")}},
	}
	reconciler, k8sClient := newTestReconciler(t, healthChecker, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	reconciler.NodeManager = &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for hibernation"}},
		},
	}

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != rolloutPollRequeue {
		t.Fatalf("expected requeue after advancing resumed hibernation step, got %s", result.RequeueAfter)
	}
	if healthChecker.podReadyCalls != 0 || healthChecker.healthCalls != 0 {
		t.Fatalf("expected no global health gate calls while resuming hibernation node preparation, got podReady=%d health=%d", healthChecker.podReadyCalls, healthChecker.healthCalls)
	}

	updatedStatefulSet := &appsv1.StatefulSet{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(statefulSet), updatedStatefulSet); err != nil {
		t.Fatalf("get updated statefulset: %v", err)
	}
	if got := derefInt32(updatedStatefulSet.Spec.Replicas); got != 2 {
		t.Fatalf("expected resumed hibernation step to reduce replicas to 2, got %d", got)
	}
}

func TestSyncReplicaStatusPublishesScaleSelector(t *testing.T) {
	cluster := managedCluster()
	target := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-rev"),
		readyPod("nifi-1", "nifi", "nifi-rev"),
		readyPod("nifi-2", "nifi", "nifi-rev"),
	}

	reconciler := &NiFiClusterReconciler{}
	reconciler.syncReplicaStatus(cluster, target, pods)

	if cluster.Status.ScaleSelector != "app=nifi" {
		t.Fatalf("expected scale selector app=nifi, got %q", cluster.Status.ScaleSelector)
	}
}

func newTestReconciler(t *testing.T, healthChecker ClusterHealthChecker, objects ...client.Object) (*NiFiClusterReconciler, client.Client) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.NiFiCluster{}).
		WithObjects(objects...).
		Build()

	return &NiFiClusterReconciler{
		Client:        k8sClient,
		APIReader:     k8sClient,
		Scheme:        scheme,
		HealthChecker: healthChecker,
		NodeManager:   &fakeNodeManager{readyImmediately: true},
	}, k8sClient
}

type deleteFailingClient struct {
	client.Client
	podName string
	err     error
}

func (c *deleteFailingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if pod, ok := obj.(*corev1.Pod); ok && pod.Name == c.podName {
		return c.err
	}
	return c.Client.Delete(ctx, obj, opts...)
}

func managedCluster() *platformv1alpha1.NiFiCluster {
	return &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "nifi",
			Namespace:  "nifi",
			Generation: 1,
		},
		Spec: platformv1alpha1.NiFiClusterSpec{
			DesiredState: platformv1alpha1.DesiredStateRunning,
			TargetRef: platformv1alpha1.TargetRef{
				Name: "nifi",
			},
			RestartPolicy: platformv1alpha1.RestartPolicy{
				TLSDrift: platformv1alpha1.TLSDiffPolicyAutoreloadThenRestartOnFailure,
			},
			Rollout: platformv1alpha1.RolloutPolicy{
				PodReadyTimeout:      metav1.Duration{Duration: time.Second},
				ClusterHealthTimeout: metav1.Duration{Duration: time.Second},
			},
			Safety: platformv1alpha1.SafetyPolicy{
				RequireClusterHealthy: true,
			},
		},
	}
}

func managedStatefulSet(name string, replicas int32, currentRevision, updateRevision string) *appsv1.StatefulSet {
	updatedReplicas := int32(0)
	if currentRevision == updateRevision {
		updatedReplicas = replicas
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nifi",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: name + "-headless",
			Replicas:    &replicas,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name: "init-conf",
						Env: []corev1.EnvVar{
							{
								Name: "KEYSTORE_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-tls"},
										Key:                  "keystorePassword",
									},
								},
							},
							{
								Name: "TRUSTSTORE_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-tls"},
										Key:                  "truststorePassword",
									},
								},
							},
						},
						Args: []string{
							`replace_property "nifi.security.keystore" "/opt/nifi/tls/keystore.p12"
replace_property "nifi.security.truststore" "/opt/nifi/tls/truststore.p12"`,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "tls",
							MountPath: "/opt/nifi/tls",
							ReadOnly:  true,
						}},
					}},
					Containers: []corev1.Container{{
						Name: "nifi",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "tls",
							MountPath: "/opt/nifi/tls",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "nifi-tls",
							},
						},
					}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: currentRevision,
			UpdateRevision:  updateRevision,
			CurrentReplicas: replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: updatedReplicas,
		},
	}
}

func readyPod(name, app, revision string) corev1.Pod {
	return readyPodAt(name, app, revision, time.Now().UTC())
}

func readyPodAt(name, app, revision string, createdAt time.Time) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "nifi",
			UID:               types.UID(name + "-uid"),
			CreationTimestamp: metav1.NewTime(createdAt),
			Labels: map[string]string{
				"app":                       app,
				controllerRevisionHashLabel: revision,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       app,
			}},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func watchedConfigMap(name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nifi",
		},
		Data: data,
	}
}

func watchedSecret(name string, secretType corev1.SecretType, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nifi",
		},
		Type: secretType,
		Data: data,
	}
}

func repositoryPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nifi",
		},
	}
}

func healthyResult(expectedReplicas int32) ClusterHealthResult {
	return ClusterHealthResult{
		ExpectedReplicas: expectedReplicas,
		ReadyPods:        expectedReplicas,
		ReachablePods:    expectedReplicas,
		ConvergedPods:    expectedReplicas,
	}
}

func healthyResultWithPods(stsName string, expectedReplicas int32) ClusterHealthResult {
	result := healthyResult(expectedReplicas)
	for ordinal := int32(0); ordinal < expectedReplicas; ordinal++ {
		result.Pods = append(result.Pods, PodHealth{
			PodName:            fmt.Sprintf("%s-%d", stsName, ordinal),
			Ready:              true,
			APIReachable:       true,
			Clustered:          true,
			ConnectedToCluster: true,
			ConnectedNodeCount: expectedReplicas,
			TotalNodeCount:     expectedReplicas,
		})
	}
	return result
}

func TestLiveTargetStateUsesBoundedReadContext(t *testing.T) {
	var observedTimeout time.Duration
	reconciler := &NiFiClusterReconciler{
		APIReader: &fakeReader{
			getFn: func(ctx context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatalf("expected live target state reads to be bounded")
				}
				observedTimeout = time.Until(deadline)
				return context.DeadlineExceeded
			},
		},
	}

	_, _, err := reconciler.liveTargetState(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "nifi"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded live target state read to return deadline exceeded, got %v", err)
	}
	if observedTimeout <= 0 || observedTimeout > controllerTargetStateReadTimeout+time.Second {
		t.Fatalf("expected bounded live target state timeout near %s, got %s", controllerTargetStateReadTimeout, observedTimeout)
	}
}

func TestLiveClusterUsesBoundedReadContext(t *testing.T) {
	var observedTimeout time.Duration
	reconciler := &NiFiClusterReconciler{
		APIReader: &fakeReader{
			getFn: func(ctx context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatalf("expected live cluster reads to be bounded")
				}
				observedTimeout = time.Until(deadline)
				return context.DeadlineExceeded
			},
		},
	}

	_, err := reconciler.liveCluster(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "nifi"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded live cluster read to return deadline exceeded, got %v", err)
	}
	if observedTimeout <= 0 || observedTimeout > controllerClusterStateReadTimeout+time.Second {
		t.Fatalf("expected bounded live cluster timeout near %s, got %s", controllerClusterStateReadTimeout, observedTimeout)
	}
}

type healthResponse struct {
	result ClusterHealthResult
	err    error
}

type fakeHealthChecker struct {
	podReadyResponses []error
	checkResponses    []healthResponse
	healthResponses   []healthResponse
	checkFn           func(context.Context, *platformv1alpha1.NiFiCluster, *appsv1.StatefulSet) (ClusterHealthResult, error)
	podReadyCalls     int
	checkCalls        int
	healthCalls       int
	podReadyReplicas  []int32
	checkReplicas     []int32
	healthReplicas    []int32
}

func (f *fakeHealthChecker) WaitForPodsReady(_ context.Context, sts *appsv1.StatefulSet, _ time.Duration) error {
	f.podReadyCalls++
	f.podReadyReplicas = append(f.podReadyReplicas, derefInt32(sts.Spec.Replicas))
	if len(f.podReadyResponses) == 0 {
		return nil
	}
	index := f.podReadyCalls - 1
	if index >= len(f.podReadyResponses) {
		index = len(f.podReadyResponses) - 1
	}
	return f.podReadyResponses[index]
}

func (f *fakeHealthChecker) WaitForClusterHealthy(_ context.Context, _ *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, _ time.Duration) (ClusterHealthResult, error) {
	f.healthCalls++
	f.healthReplicas = append(f.healthReplicas, derefInt32(sts.Spec.Replicas))
	if len(f.healthResponses) == 0 {
		return healthyResult(3), nil
	}
	index := f.healthCalls - 1
	if index >= len(f.healthResponses) {
		index = len(f.healthResponses) - 1
	}
	response := f.healthResponses[index]
	return response.result, response.err
}

func (f *fakeHealthChecker) CheckClusterHealth(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet) (ClusterHealthResult, error) {
	f.checkCalls++
	f.checkReplicas = append(f.checkReplicas, derefInt32(sts.Spec.Replicas))
	if f.checkFn != nil {
		return f.checkFn(ctx, cluster, sts)
	}
	if len(f.checkResponses) == 0 {
		return healthyResultWithPods(sts.Name, derefInt32(sts.Spec.Replicas)), nil
	}
	index := f.checkCalls - 1
	if index >= len(f.checkResponses) {
		index = len(f.checkResponses) - 1
	}
	response := f.checkResponses[index]
	return response.result, response.err
}

type fakeReader struct {
	getFn  func(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error
	listFn func(context.Context, client.ObjectList, ...client.ListOption) error
}

func (f *fakeReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if f.getFn != nil {
		return f.getFn(ctx, key, obj, opts...)
	}
	return nil
}

func (f *fakeReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if f.listFn != nil {
		return f.listFn(ctx, list, opts...)
	}
	return nil
}

type nodePreparationResponse struct {
	result NodePreparationResult
	err    error
}

type fakeNodeManager struct {
	readyImmediately bool
	responses        []nodePreparationResponse
	calls            int
	pods             []string
	purposes         []platformv1alpha1.NodeOperationPurpose
	currentStates    []platformv1alpha1.NodeOperationStatus
}

func (f *fakeNodeManager) PreparePodForOperation(_ context.Context, _ *platformv1alpha1.NiFiCluster, _ *appsv1.StatefulSet, _ []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose, current platformv1alpha1.NodeOperationStatus, _ time.Duration) (NodePreparationResult, error) {
	f.calls++
	f.pods = append(f.pods, pod.Name)
	f.purposes = append(f.purposes, purpose)
	f.currentStates = append(f.currentStates, current)

	if len(f.responses) == 0 {
		if f.readyImmediately {
			return NodePreparationResult{Ready: true, Message: "node prepared"}, nil
		}
		return NodePreparationResult{}, nil
	}

	index := f.calls - 1
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	response := f.responses[index]
	return response.result, response.err
}
