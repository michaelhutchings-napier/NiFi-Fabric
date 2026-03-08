package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DesiredState describes the coarse runtime intent for a NiFi cluster.
type DesiredState string

const (
	DesiredStateRunning    DesiredState = "Running"
	DesiredStateHibernated DesiredState = "Hibernated"
)

// TLSDiffPolicy defines how the controller should react to TLS drift.
type TLSDiffPolicy string

const (
	TLSDiffPolicyAutoreloadThenRestartOnFailure TLSDiffPolicy = "AutoreloadThenRestartOnFailure"
	TLSDiffPolicyAlwaysRestart                  TLSDiffPolicy = "AlwaysRestart"
	TLSDiffPolicyObserveOnly                    TLSDiffPolicy = "ObserveOnly"
)

// OperationPhase describes the phase of a tracked lifecycle action.
type OperationPhase string

const (
	OperationPhasePending   OperationPhase = "Pending"
	OperationPhaseRunning   OperationPhase = "Running"
	OperationPhaseSucceeded OperationPhase = "Succeeded"
	OperationPhaseFailed    OperationPhase = "Failed"
)

type TargetRef struct {
	Name string `json:"name"`
}

type RestartTriggers struct {
	ConfigMaps []corev1.LocalObjectReference `json:"configMaps,omitempty"`
	Secrets    []corev1.LocalObjectReference `json:"secrets,omitempty"`
}

type RestartPolicy struct {
	TLSDrift TLSDiffPolicy `json:"tlsDrift,omitempty"`
}

type RolloutPolicy struct {
	MinReadySeconds      int32           `json:"minReadySeconds,omitempty"`
	PodReadyTimeout      metav1.Duration `json:"podReadyTimeout,omitempty"`
	ClusterHealthTimeout metav1.Duration `json:"clusterHealthTimeout,omitempty"`
}

type HibernationPolicy struct {
	OffloadTimeout metav1.Duration `json:"offloadTimeout,omitempty"`
}

type SafetyPolicy struct {
	RequireClusterHealthy bool `json:"requireClusterHealthy,omitempty"`
}

type NiFiClusterSpec struct {
	TargetRef       TargetRef         `json:"targetRef"`
	DesiredState    DesiredState      `json:"desiredState"`
	Suspend         bool              `json:"suspend"`
	RestartTriggers RestartTriggers   `json:"restartTriggers,omitempty"`
	RestartPolicy   RestartPolicy     `json:"restartPolicy,omitempty"`
	Rollout         RolloutPolicy     `json:"rollout,omitempty"`
	Hibernation     HibernationPolicy `json:"hibernation,omitempty"`
	Safety          SafetyPolicy      `json:"safety,omitempty"`
}

type ReplicaStatus struct {
	Desired int32 `json:"desired,omitempty"`
	Ready   int32 `json:"ready,omitempty"`
	Updated int32 `json:"updated,omitempty"`
}

type ClusterNodesStatus struct {
	Connected    int32 `json:"connected,omitempty"`
	Disconnected int32 `json:"disconnected,omitempty"`
	Offloaded    int32 `json:"offloaded,omitempty"`
}

type HibernationStatus struct {
	LastRunningReplicas int32 `json:"lastRunningReplicas,omitempty"`
}

type LastOperation struct {
	Type        string         `json:"type,omitempty"`
	Phase       OperationPhase `json:"phase,omitempty"`
	StartedAt   *metav1.Time   `json:"startedAt,omitempty"`
	CompletedAt *metav1.Time   `json:"completedAt,omitempty"`
	Message     string         `json:"message,omitempty"`
}

type RolloutTrigger string

const (
	RolloutTriggerStatefulSetRevision RolloutTrigger = "StatefulSetRevision"
	RolloutTriggerConfigDrift         RolloutTrigger = "ConfigDrift"
)

type RolloutStatus struct {
	Trigger          RolloutTrigger `json:"trigger,omitempty"`
	StartedAt        *metav1.Time   `json:"startedAt,omitempty"`
	TargetConfigHash string         `json:"targetConfigHash,omitempty"`
}

type NiFiClusterStatus struct {
	ObservedGeneration          int64              `json:"observedGeneration,omitempty"`
	ObservedStatefulSetRevision string             `json:"observedStatefulSetRevision,omitempty"`
	ObservedConfigHash          string             `json:"observedConfigHash,omitempty"`
	ObservedCertificateHash     string             `json:"observedCertificateHash,omitempty"`
	Rollout                     RolloutStatus      `json:"rollout,omitempty"`
	Replicas                    ReplicaStatus      `json:"replicas,omitempty"`
	ClusterNodes                ClusterNodesStatus `json:"clusterNodes,omitempty"`
	Hibernation                 HibernationStatus  `json:"hibernation,omitempty"`
	LastOperation               LastOperation      `json:"lastOperation,omitempty"`
	Conditions                  []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nificlusters,scope=Namespaced,categories=nifi
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".spec.desiredState"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.replicas.ready"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NiFiCluster is the thin operational API for a chart-managed NiFi StatefulSet.
type NiFiCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NiFiClusterSpec   `json:"spec,omitempty"`
	Status NiFiClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NiFiClusterList contains a list of NiFiCluster.
type NiFiClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NiFiCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NiFiCluster{}, &NiFiClusterList{})
}
