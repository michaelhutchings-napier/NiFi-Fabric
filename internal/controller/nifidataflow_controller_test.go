package controller

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

func TestNiFiDataflowReconcileBlocksWhenClusterMissing(t *testing.T) {
	reconciler, k8sClient := newTestDataflowReconciler(t, dataflowFixture())

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseBlocked {
		t.Fatalf("expected blocked phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionTargetResolved); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected target resolved false condition")
	}
}

func TestNiFiDataflowReconcilePublishesBridgeConfigMapWhenTargetSupportsBridge(t *testing.T) {
	cluster := &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: platformv1alpha1.NiFiClusterSpec{
			TargetRef:    platformv1alpha1.TargetRef{Name: "nifi"},
			DesiredState: platformv1alpha1.DesiredStateRunning,
		},
	}
	target := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "nifidataflow-bridge",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-nifidataflows"},
								},
							},
						},
					},
				},
			},
		},
	}
	reconciler, k8sClient := newTestDataflowReconciler(t, dataflowFixture(), cluster, target)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseProgressing {
		t.Fatalf("expected progressing phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionProgressing); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected progressing true condition")
	}

	configMap := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "nifi-nifidataflows"}, configMap); err != nil {
		t.Fatalf("get bridge configmap: %v", err)
	}
	if !strings.Contains(configMap.Data["imports.json"], `"name": "orders-ingest"`) {
		t.Fatalf("expected bridge configmap imports.json to contain dataflow entry\n%s", configMap.Data["imports.json"])
	}
}

func TestNiFiDataflowReconcileProjectsReadyRuntimeStatus(t *testing.T) {
	cluster := &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: platformv1alpha1.NiFiClusterSpec{
			TargetRef:    platformv1alpha1.TargetRef{Name: "nifi"},
			DesiredState: platformv1alpha1.DesiredStateRunning,
		},
	}
	target := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "nifidataflow-bridge",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-nifidataflows"},
								},
							},
						},
					},
				},
			},
		},
	}
	statusConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi-nifidataflows-status", Namespace: "nifi"},
		Data: map[string]string{
			"status.json": `{
  "status": "ok",
  "imports": [
    {
      "name": "orders-ingest",
      "status": "ok",
      "action": "created",
      "processGroupId": "pg-orders",
      "registryClientName": "github-main",
      "registryClientId": "registry-1",
      "bucket": "platform-flows",
      "bucketId": "bucket-1",
      "flowName": "orders-ingest",
      "flowId": "flow-1",
      "resolvedVersion": "12",
      "actualVersion": "12"
    }
  ]
}`,
		},
	}
	reconciler, k8sClient := newTestDataflowReconciler(t, dataflowFixture(), cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseReady {
		t.Fatalf("expected ready phase, got %q", updated.Status.Phase)
	}
	if updated.Status.ProcessGroupID != "pg-orders" {
		t.Fatalf("expected process group id to be projected from runtime status, got %q", updated.Status.ProcessGroupID)
	}
	if updated.Status.ObservedVersion != "12" {
		t.Fatalf("expected observed version to be projected from runtime status, got %q", updated.Status.ObservedVersion)
	}
	if updated.Status.LastSuccessfulVersion != "12" {
		t.Fatalf("expected last successful version to be projected from runtime status, got %q", updated.Status.LastSuccessfulVersion)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionAvailable); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected available true condition")
	}
}

func TestNiFiDataflowReconcileProjectsBlockedRuntimeStatus(t *testing.T) {
	cluster := &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: platformv1alpha1.NiFiClusterSpec{
			TargetRef:    platformv1alpha1.TargetRef{Name: "nifi"},
			DesiredState: platformv1alpha1.DesiredStateRunning,
		},
	}
	target := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "nifidataflow-bridge",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-nifidataflows"},
								},
							},
						},
					},
				},
			},
		},
	}
	dataflow := dataflowFixture()
	dataflow.Spec.Target.ParameterContextRef = &platformv1alpha1.ParameterContextRef{Name: "orders-prod"}
	statusConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi-nifidataflows-status", Namespace: "nifi"},
		Data: map[string]string{
			"status.json": `{
  "status": "blocked",
  "imports": [
    {
      "name": "orders-ingest",
      "status": "blocked",
      "reason": "declared Parameter Context \"orders-prod\" does not exist in NiFi yet",
      "registryClientName": "github-main",
      "registryClientId": "registry-1",
      "bucket": "platform-flows",
      "bucketId": "bucket-1",
      "flowName": "orders-ingest",
      "flowId": "flow-1",
      "resolvedVersion": "12"
    }
  ]
}`,
		},
	}
	reconciler, k8sClient := newTestDataflowReconciler(t, dataflow, cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseBlocked {
		t.Fatalf("expected blocked phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionDegraded); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected degraded true condition")
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionParameterContextReady); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected parameter context ready false condition")
	}
}

func TestNiFiDataflowReconcileBlocksWhenTargetDoesNotMountBridge(t *testing.T) {
	cluster := &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
		Spec: platformv1alpha1.NiFiClusterSpec{
			TargetRef:    platformv1alpha1.TargetRef{Name: "nifi"},
			DesiredState: platformv1alpha1.DesiredStateRunning,
		},
	}
	target := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "nifi", Namespace: "nifi"},
	}
	reconciler, k8sClient := newTestDataflowReconciler(t, dataflowFixture(), cluster, target)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseBlocked {
		t.Fatalf("expected blocked phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionTargetResolved); condition == nil || condition.Reason != "BridgeNotMounted" {
		t.Fatalf("expected target resolved condition to explain missing bridge mount")
	}
}

func reconcileDataflow(t *testing.T, reconciler *NiFiDataflowReconciler) {
	t.Helper()

	if _, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{
		Namespace: "nifi",
		Name:      "orders-ingest",
	}}); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
}

func newTestDataflowReconciler(t *testing.T, objects ...client.Object) (*NiFiDataflowReconciler, client.Client) {
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
		WithStatusSubresource(&platformv1alpha1.NiFiDataflow{}, &platformv1alpha1.NiFiCluster{}).
		WithObjects(objects...).
		Build()

	return &NiFiDataflowReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		APIReader: k8sClient,
	}, k8sClient
}

func dataflowFixture() *platformv1alpha1.NiFiDataflow {
	return &platformv1alpha1.NiFiDataflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orders-ingest",
			Namespace: "nifi",
		},
		Spec: platformv1alpha1.NiFiDataflowSpec{
			ClusterRef: platformv1alpha1.NiFiClusterRef{Name: "nifi"},
			Source: platformv1alpha1.DataflowSource{
				RegistryClient: platformv1alpha1.RegistryClientRef{Name: "github-main"},
				Bucket:         "platform-flows",
				Flow:           "orders-ingest",
				Version:        "12",
			},
			Target: platformv1alpha1.DataflowTarget{
				RootChildProcessGroupName: "orders-ingest",
			},
		},
	}
}
