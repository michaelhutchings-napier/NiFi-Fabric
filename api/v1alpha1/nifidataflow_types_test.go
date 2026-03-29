package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNiFiDataflowInitializeConditionsSeedsKnownConditions(t *testing.T) {
	dataflow := &NiFiDataflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "example",
			Generation: 4,
		},
	}

	dataflow.InitializeConditions()

	if got := len(dataflow.Status.Conditions); got != len(nifiDataflowConditionTypes) {
		t.Fatalf("expected %d conditions, got %d", len(nifiDataflowConditionTypes), got)
	}

	for _, conditionType := range nifiDataflowConditionTypes {
		condition := dataflow.GetCondition(conditionType)
		if condition == nil {
			t.Fatalf("expected condition %q to exist", conditionType)
		}
		if condition.ObservedGeneration != dataflow.Generation {
			t.Fatalf("expected condition %q observed generation %d, got %d", conditionType, dataflow.Generation, condition.ObservedGeneration)
		}
	}
}
