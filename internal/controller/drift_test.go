package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

func TestAggregateConfigHashIsStableAcrossInputOrder(t *testing.T) {
	left := aggregateConfigHash(
		[]corev1.ConfigMap{
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Data: map[string]string{"z": "1", "a": "2"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Data: map[string]string{"k": "v"}},
		},
		[]corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-b"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"b": []byte("2"), "a": []byte("1")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-a"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"x": []byte("y")}},
		},
	)
	right := aggregateConfigHash(
		[]corev1.ConfigMap{
			{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Data: map[string]string{"k": "v"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Data: map[string]string{"a": "2", "z": "1"}},
		},
		[]corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-a"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"x": []byte("y")}},
			{ObjectMeta: metav1.ObjectMeta{Name: "secret-b"}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"a": []byte("1"), "b": []byte("2")}},
		},
	)

	if left != right {
		t.Fatalf("expected stable config hash, got %q and %q", left, right)
	}
}

func TestComputeWatchedResourceDriftSeparatesTLSAndConfigSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add platform scheme: %v", err)
	}

	cluster := managedCluster()
	cluster.Spec.RestartTriggers.ConfigMaps = []corev1.LocalObjectReference{{Name: "nifi-config"}}
	cluster.Spec.RestartTriggers.Secrets = []corev1.LocalObjectReference{{Name: "nifi-auth"}, {Name: "nifi-tls"}}
	cluster.Status.ObservedConfigHash = "old-config-hash"
	cluster.Status.ObservedCertificateHash = "old-certificate-hash"

	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	configMap := watchedConfigMap("nifi-config", map[string]string{"nifi.properties.overrides": "foo=bar"})
	authSecret := watchedSecret("nifi-auth", corev1.SecretTypeOpaque, map[string][]byte{"password": []byte("changed")})
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{"tls.p12": []byte("changed")})

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster, statefulSet, configMap, authSecret, tlsSecret).
		Build()

	reconciler := &NiFiClusterReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}

	drift, err := reconciler.computeWatchedResourceDrift(context.Background(), cluster, statefulSet)
	if err != nil {
		t.Fatalf("compute drift: %v", err)
	}

	if !drift.ConfigDrift {
		t.Fatalf("expected config drift")
	}
	if !drift.CertificateDrift {
		t.Fatalf("expected certificate drift")
	}
	if got, want := drift.ConfigRefs, []string{"ConfigMap/nifi-config", "Secret/nifi-auth"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected config refs: %#v", got)
	}
	if got, want := drift.CertificateRefs, []string{"Secret/nifi-tls"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected certificate refs: %#v", got)
	}
}

func TestBuildRolloutPlanUsesStartedAtForConfigDrift(t *testing.T) {
	startedAt := metav1.NewTime(time.Now().Add(-time.Minute).UTC())
	statefulSet := managedStatefulSet("nifi", 3, "nifi-rev", "nifi-rev")
	pods := []corev1.Pod{
		readyPodAt("nifi-0", "nifi", "nifi-rev", startedAt.Time.Add(-time.Minute)),
		readyPodAt("nifi-1", "nifi", "nifi-rev", startedAt.Time.Add(time.Minute)),
		readyPodAt("nifi-2", "nifi", "nifi-rev", startedAt.Time.Add(-time.Minute)),
	}

	plan := BuildRolloutPlan(statefulSet, pods, platformv1alpha1.RolloutStatus{
		Trigger:   platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt: &startedAt,
	})

	if plan.Trigger != platformv1alpha1.RolloutTriggerConfigDrift {
		t.Fatalf("expected config drift trigger, got %q", plan.Trigger)
	}
	if got := podNames(plan.OutdatedPods); got != "nifi-0,nifi-2" {
		t.Fatalf("unexpected outdated pods: %s", got)
	}
}
