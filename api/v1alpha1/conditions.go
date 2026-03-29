package v1alpha1

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConditionTargetResolved = "TargetResolved"
	ConditionSecretsReady   = "SecretsReady"
	ConditionTLSReady       = "TLSMaterialReady"
	ConditionAvailable      = "Available"
	ConditionProgressing    = "Progressing"
	ConditionDegraded       = "Degraded"
	ConditionHibernated     = "Hibernated"
)

var defaultConditionTypes = []string{
	ConditionTargetResolved,
	ConditionSecretsReady,
	ConditionTLSReady,
	ConditionAvailable,
	ConditionProgressing,
	ConditionDegraded,
	ConditionHibernated,
}

// InitializeConditions ensures the known condition set exists with stable defaults.
func (n *NiFiCluster) InitializeConditions() {
	for _, conditionType := range defaultConditionTypes {
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

func (n *NiFiCluster) GetCondition(conditionType string) *metav1.Condition {
	for i := range n.Status.Conditions {
		if n.Status.Conditions[i].Type == conditionType {
			return &n.Status.Conditions[i]
		}
	}
	return nil
}

func (n *NiFiCluster) SetCondition(condition metav1.Condition) {
	condition.ObservedGeneration = n.Generation
	apimeta.SetStatusCondition(&n.Status.Conditions, condition)
}
