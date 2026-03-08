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

func TestReconcileRestoreFallsBackToSingleReplicaWhenNoLastRunningReplicaExists(t *testing.T) {
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
					Message: "Requested NiFi disconnect for node node-2",
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
		t.Fatalf("expected requeue while preparing node, got %s", firstResult.RequeueAfter)
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

	reconciler, k8sClient := newTestReconciler(t, &fakeHealthChecker{
		podReadyResponses: []error{nil, nil},
		healthResponses:   []healthResponse{{result: healthyResult(3)}, {result: healthyResult(2)}},
	}, cluster, statefulSet, &pods[0], &pods[1], &pods[2])
	nodeManager := &fakeNodeManager{
		responses: []nodePreparationResponse{
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-2 is OFFLOADED and ready for hibernation"}},
			{result: NodePreparationResult{Ready: true, Message: "NiFi node node-1 is OFFLOADED and ready for hibernation"}},
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
		NodeManager:   &fakeNodeManager{readyImmediately: true},
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

type healthResponse struct {
	result ClusterHealthResult
	err    error
}

type fakeHealthChecker struct {
	podReadyResponses []error
	checkResponses    []healthResponse
	healthResponses   []healthResponse
	podReadyCalls     int
	checkCalls        int
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

func (f *fakeHealthChecker) CheckClusterHealth(_ context.Context, _ *platformv1alpha1.NiFiCluster, _ *appsv1.StatefulSet) (ClusterHealthResult, error) {
	f.checkCalls++
	if len(f.checkResponses) == 0 {
		return healthyResult(3), nil
	}
	index := f.checkCalls - 1
	if index >= len(f.checkResponses) {
		index = len(f.checkResponses) - 1
	}
	response := f.checkResponses[index]
	return response.result, response.err
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
