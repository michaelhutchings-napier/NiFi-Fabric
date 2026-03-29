package v1alpha1

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionSourceResolved        = "SourceResolved"
	ConditionParameterContextReady = "ParameterContextReady"
)

var nifiDataflowConditionTypes = []string{
	ConditionSourceResolved,
	ConditionTargetResolved,
	ConditionParameterContextReady,
	ConditionAvailable,
	ConditionProgressing,
	ConditionDegraded,
}

// InitializeConditions ensures the known condition set exists with stable defaults.
func (n *NiFiDataflow) InitializeConditions() {
	for _, conditionType := range nifiDataflowConditionTypes {
		if n.GetCondition(conditionType) != nil {
			continue
		}

		n.Status.Conditions = append(n.Status.Conditions, metav1.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionUnknown,
			Reason:             "Initializing",
			Message:            "Condition has not been reconciled yet",
			LastTransitionTime: metav1.Now(),
			ObservedGeneration: n.Generation,
		})
	}
}

func (n *NiFiDataflow) GetCondition(conditionType string) *metav1.Condition {
	for i := range n.Status.Conditions {
		if n.Status.Conditions[i].Type == conditionType {
			return &n.Status.Conditions[i]
		}
	}
	return nil
}

func (n *NiFiDataflow) SetCondition(condition metav1.Condition) {
	condition.ObservedGeneration = n.Generation
	apimeta.SetStatusCondition(&n.Status.Conditions, condition)
}
