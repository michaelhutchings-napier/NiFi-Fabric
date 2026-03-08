package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

func TestSyncFromStatefulSetCopiesBasicStatus(t *testing.T) {
	replicas := int32(3)
	cluster := &platformv1alpha1.NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "example",
			Generation: 1,
		},
		Spec: platformv1alpha1.NiFiClusterSpec{
			DesiredState: platformv1alpha1.DesiredStateRunning,
		},
	}
	cluster.InitializeConditions()

	sts := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:   3,
			UpdatedReplicas: 3,
			UpdateRevision:  "nifi-123",
		},
	}

	reconciler := &NiFiClusterReconciler{}
	reconciler.syncFromStatefulSet(cluster, sts)

	if cluster.Status.ObservedStatefulSetRevision != "nifi-123" {
		t.Fatalf("expected revision to be copied")
	}
	if cluster.Status.Replicas.Desired != 3 || cluster.Status.Replicas.Ready != 3 {
		t.Fatalf("expected replica counts to be copied")
	}
	if condition := cluster.GetCondition(platformv1alpha1.ConditionAvailable); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("expected available condition true")
	}
}
