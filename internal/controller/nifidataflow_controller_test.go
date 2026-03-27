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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

func TestNiFiDataflowReconcileBlocksWhenClusterMissing(t *testing.T) {
	reconciler, k8sClient, _ := newTestDataflowReconciler(t, dataflowFixture())

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
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflowFixture(), cluster, target)

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
	expectEventContains(t, recorder, "Normal RuntimeImportProgressing", "controller bridge is published")
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
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflowFixture(), cluster, target, statusConfigMap)

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
	if updated.Status.Ownership.State != platformv1alpha1.DataflowOwnershipStateManaged {
		t.Fatalf("expected ownership state Managed, got %q", updated.Status.Ownership.State)
	}
	if !strings.Contains(updated.Status.LastOperation.Message, "process group pg-orders") {
		t.Fatalf("expected last operation to include projected process group details, got %q", updated.Status.LastOperation.Message)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionAvailable); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected available true condition")
	} else if !strings.Contains(condition.Message, "version 12") {
		t.Fatalf("expected available condition to include runtime version detail, got %q", condition.Message)
	}
	expectEventContains(t, recorder, "Normal RuntimeImportReady", "process group pg-orders", "version 12")
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
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflow, cluster, target, statusConfigMap)

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
	if !strings.Contains(updated.Status.LastOperation.Message, "version 12") {
		t.Fatalf("expected blocked last operation to include runtime version detail, got %q", updated.Status.LastOperation.Message)
	}
	expectEventContains(t, recorder, "Warning RuntimeImportBlocked", "Parameter Context attachment", "version 12")
}

func TestNiFiDataflowReconcileSurfacesRetainedOwnedImportWarnings(t *testing.T) {
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
  ],
  "retainedOwnedImports": [
    {
      "name": "legacy-payments",
      "targetRootProcessGroupName": "legacy-payments",
      "processGroupId": "pg-legacy",
      "action": "retained",
      "status": "ok",
      "reason": "removed imports are retained in NiFi and no longer reconciled in this slice"
    }
  ]
}`,
		},
	}
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflowFixture(), cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseReady {
		t.Fatalf("expected ready phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionDegraded); condition == nil || condition.Reason != "RetainedOwnedImportsPresent" || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected degraded warning for retained owned imports, got %#v", condition)
	} else if !strings.Contains(condition.Message, "legacy-payments") {
		t.Fatalf("expected retained import warning to mention legacy-payments, got %q", condition.Message)
	}
	if len(updated.Status.Warnings.RetainedOwnedImports) != 1 || updated.Status.Warnings.RetainedOwnedImports[0].Name != "legacy-payments" {
		t.Fatalf("expected retained-owned-import warning summary in status, got %#v", updated.Status.Warnings.RetainedOwnedImports)
	}
	expectEventContains(t, recorder, "Warning RetainedOwnedImportsPresent", "legacy-payments")
}

func TestNiFiDataflowReconcileEmitsRetainedOwnedImportsClearedEvent(t *testing.T) {
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
  ],
  "retainedOwnedImports": [
    {
      "name": "legacy-payments",
      "targetRootProcessGroupName": "legacy-payments",
      "processGroupId": "pg-legacy",
      "action": "retained",
      "status": "ok",
      "reason": "removed imports are retained in NiFi and no longer reconciled in this slice"
    }
  ]
}`,
		},
	}
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflowFixture(), cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)
	expectEventContains(t, recorder, "Warning RetainedOwnedImportsPresent", "legacy-payments")

	updatedConfigMap := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "nifi-nifidataflows-status"}, updatedConfigMap); err != nil {
		t.Fatalf("get status configmap: %v", err)
	}
	updatedConfigMap.Data["status.json"] = `{
  "status": "ok",
  "imports": [
    {
      "name": "orders-ingest",
      "status": "ok",
      "action": "unchanged",
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
  ],
  "retainedOwnedImports": []
}`
	if err := k8sClient.Update(context.Background(), updatedConfigMap); err != nil {
		t.Fatalf("update status configmap: %v", err)
	}

	reconcileDataflow(t, reconciler)
	expectEventContains(t, recorder, "Normal RetainedOwnedImportsCleared", "warning cleared")

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if len(updated.Status.Warnings.RetainedOwnedImports) != 0 {
		t.Fatalf("expected retained-owned-import warnings to clear from status, got %#v", updated.Status.Warnings.RetainedOwnedImports)
	}
}

func TestNiFiDataflowReconcileClassifiesOwnershipAdoptionConflict(t *testing.T) {
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
  "status": "blocked",
  "imports": [
    {
      "name": "orders-ingest",
      "status": "blocked",
      "reason": "declared target root child process group \"orders-ingest\" already exists in NiFi without the product ownership marker; operator-owned targets are not adopted automatically",
      "targetRootProcessGroupName": "orders-ingest"
    }
  ]
}`,
		},
	}
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflowFixture(), cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)

	updated := &platformv1alpha1.NiFiDataflow{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "orders-ingest"}, updated); err != nil {
		t.Fatalf("get updated dataflow: %v", err)
	}
	if updated.Status.Phase != platformv1alpha1.DataflowPhaseBlocked {
		t.Fatalf("expected blocked phase, got %q", updated.Status.Phase)
	}
	if condition := updated.GetCondition(platformv1alpha1.ConditionTargetResolved); condition == nil || condition.Reason != "AdoptionRefused" || condition.Status != metav1.ConditionFalse {
		t.Fatalf("expected target resolved false adoption-refused condition, got %#v", condition)
	}
	if updated.Status.Ownership.State != platformv1alpha1.DataflowOwnershipStateAdoptionRefused {
		t.Fatalf("expected ownership state AdoptionRefused, got %q", updated.Status.Ownership.State)
	}
	expectEventContains(t, recorder, "Warning AdoptionRefused", "will not be adopted automatically")
}

func TestNiFiDataflowReconcileDoesNotSpamBlockedEventsWhenOnlyDetailTextChanges(t *testing.T) {
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
	reconciler, k8sClient, recorder := newTestDataflowReconciler(t, dataflow, cluster, target, statusConfigMap)

	reconcileDataflow(t, reconciler)
	reconcileDataflow(t, reconciler)
	expectEventContains(t, recorder, "Warning RuntimeImportBlocked", "Parameter Context attachment")

	updatedConfigMap := &corev1.ConfigMap{}
	if err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: "nifi", Name: "nifi-nifidataflows-status"}, updatedConfigMap); err != nil {
		t.Fatalf("get status configmap: %v", err)
	}
	updatedConfigMap.Data["status.json"] = `{
  "status": "blocked",
  "imports": [
    {
      "name": "orders-ingest",
      "status": "blocked",
      "reason": "declared Parameter Context \"orders-prod\" still does not exist after retry",
      "registryClientName": "github-main",
      "registryClientId": "registry-1",
      "bucket": "platform-flows",
      "bucketId": "bucket-1",
      "flowName": "orders-ingest",
      "flowId": "flow-1",
      "resolvedVersion": "12"
    }
  ]
}`
	if err := k8sClient.Update(context.Background(), updatedConfigMap); err != nil {
		t.Fatalf("update status configmap: %v", err)
	}

	reconcileDataflow(t, reconciler)
	expectNoEvent(t, recorder)
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
	reconciler, k8sClient, _ := newTestDataflowReconciler(t, dataflowFixture(), cluster, target)

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

func newTestDataflowReconciler(t *testing.T, objects ...client.Object) (*NiFiDataflowReconciler, client.Client, *record.FakeRecorder) {
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

	recorder := record.NewFakeRecorder(20)

	return &NiFiDataflowReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		APIReader: k8sClient,
		Recorder:  recorder,
	}, k8sClient, recorder
}

func expectEventContains(t *testing.T, recorder *record.FakeRecorder, want ...string) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		for _, fragment := range want {
			if !strings.Contains(event, fragment) {
				t.Fatalf("expected event %q to contain %q", event, fragment)
			}
		}
	default:
		t.Fatalf("expected an event containing %v but recorder was empty", want)
	}
}

func expectNoEvent(t *testing.T, recorder *record.FakeRecorder) {
	t.Helper()

	select {
	case event := <-recorder.Events:
		t.Fatalf("expected no event but received %q", event)
	default:
	}
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
