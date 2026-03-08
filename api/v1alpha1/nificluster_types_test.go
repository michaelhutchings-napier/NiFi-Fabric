package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInitializeConditionsSeedsKnownConditions(t *testing.T) {
	cluster := &NiFiCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "example",
			Generation: 3,
		},
	}

	cluster.InitializeConditions()

	if got := len(cluster.Status.Conditions); got != len(defaultConditionTypes) {
		t.Fatalf("expected %d conditions, got %d", len(defaultConditionTypes), got)
	}

	for _, conditionType := range defaultConditionTypes {
		condition := cluster.GetCondition(conditionType)
		if condition == nil {
			t.Fatalf("expected condition %q to exist", conditionType)
		}
		if condition.ObservedGeneration != cluster.Generation {
			t.Fatalf("expected condition %q observed generation %d, got %d", conditionType, cluster.Generation, condition.ObservedGeneration)
		}
	}
}
