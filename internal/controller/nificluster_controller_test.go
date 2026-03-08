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

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{})
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

func TestReconcileRecordsTLSDriftWithoutRestart(t *testing.T) {
	ctx := context.Background()
	cluster := managedCluster()
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-tls"}}
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"

	replicas := int32(3)
	statefulSet := managedStatefulSet("nifi", replicas, "nifi-rev", "nifi-rev")
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
		healthResponses: []healthResponse{{result: healthyResult(3)}},
	}, cluster, statefulSet, tlsSecret, &pods[0], &pods[1], &pods[2])

	result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)})
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("expected no requeue for observe-only TLS drift, got %s", result.RequeueAfter)
	}
	for _, podName := range []string{"nifi-0", "nifi-1", "nifi-2"} {
		pod := &corev1.Pod{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: podName}, pod); err != nil {
			t.Fatalf("expected pod %s to remain present: %v", podName, err)
		}
	}

	updatedCluster := &platformv1alpha1.NiFiCluster{}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(cluster), updatedCluster); err != nil {
		t.Fatalf("get updated cluster: %v", err)
	}
	if updatedCluster.Status.ObservedCertificateHash != "old-certificate-hash" {
		t.Fatalf("expected observed certificate hash to stay unchanged until TLS policy is implemented")
	}
	if updatedCluster.Status.Rollout.Trigger != "" {
		t.Fatalf("expected no rollout trigger for TLS drift, got %q", updatedCluster.Status.Rollout.Trigger)
	}
	progressing := updatedCluster.GetCondition(platformv1alpha1.ConditionProgressing)
	if progressing == nil || progressing.Reason != "CertificateDriftObserved" {
		t.Fatalf("expected certificate drift progressing reason, got %#v", progressing)
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
