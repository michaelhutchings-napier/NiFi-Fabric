package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

func TestBuildRolloutPlanSelectsHighestOrdinalOutdatedPod(t *testing.T) {
	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-old", "nifi-new")
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-new"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}

	plan := BuildRolloutPlan(statefulSet, pods)
	next := plan.NextPodToDelete()
	if next == nil {
		t.Fatalf("expected a pod to delete")
	}
	if next.Name != "nifi-2" {
		t.Fatalf("expected highest ordinal outdated pod, got %s", next.Name)
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
		healthResponses: []healthResponse{{
			result: healthyResult(3),
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
		t.Fatalf("expected requeue after pod deletion, got %s", result.RequeueAfter)
	}

	deletedPod := &corev1.Pod{}
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: "nifi-2"}, deletedPod)
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
		healthResponses: []healthResponse{{
			result: healthyResult(3),
		}},
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

func TestReconcileResumesFromCurrentStateAfterRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Status.ObservedStatefulSetRevision = "nifi-old"
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
		healthResponses: []healthResponse{{
			result: healthyResult(3),
		}},
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
		Scheme:        scheme,
		HealthChecker: healthChecker,
	}, k8sClient
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
			},
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: currentRevision,
			UpdateRevision:  updateRevision,
			CurrentReplicas: replicas,
			ReadyReplicas:   replicas,
			UpdatedReplicas: 0,
		},
	}
}

func readyPod(name, app, revision string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "nifi",
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

func healthyResult(expectedReplicas int32) ClusterHealthResult {
	return ClusterHealthResult{
		ExpectedReplicas: expectedReplicas,
		ReadyPods:        expectedReplicas,
		ReachablePods:    expectedReplicas,
		ConvergedPods:    expectedReplicas,
	}
}

type healthResponse struct {
	result ClusterHealthResult
	err    error
}

type fakeHealthChecker struct {
	podReadyResponses []error
	healthResponses   []healthResponse
	podReadyCalls     int
	healthCalls       int
}

func (f *fakeHealthChecker) WaitForPodsReady(_ context.Context, _ *appsv1.StatefulSet, _ time.Duration) error {
	f.podReadyCalls++
	if len(f.podReadyResponses) == 0 {
		return nil
	}
	index := f.podReadyCalls - 1
	if index >= len(f.podReadyResponses) {
		index = len(f.podReadyResponses) - 1
	}
	return f.podReadyResponses[index]
}

func (f *fakeHealthChecker) WaitForClusterHealthy(_ context.Context, _ *platformv1alpha1.NiFiCluster, _ *appsv1.StatefulSet, _ time.Duration) (ClusterHealthResult, error) {
	f.healthCalls++
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
