package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Replace;DrainAndReplace
type DataflowRolloutStrategy string

const (
	DataflowRolloutStrategyReplace         DataflowRolloutStrategy = "Replace"
	DataflowRolloutStrategyDrainAndReplace DataflowRolloutStrategy = "DrainAndReplace"
)

// +kubebuilder:validation:Enum=Once;OnChange
type DataflowSyncMode string

const (
	DataflowSyncModeOnce     DataflowSyncMode = "Once"
	DataflowSyncModeOnChange DataflowSyncMode = "OnChange"
)

// +kubebuilder:validation:Enum=Pending;Importing;Ready;Progressing;Blocked;Failed
type DataflowPhase string

const (
	DataflowPhasePending     DataflowPhase = "Pending"
	DataflowPhaseImporting   DataflowPhase = "Importing"
	DataflowPhaseReady       DataflowPhase = "Ready"
	DataflowPhaseProgressing DataflowPhase = "Progressing"
	DataflowPhaseBlocked     DataflowPhase = "Blocked"
	DataflowPhaseFailed      DataflowPhase = "Failed"
)

type NiFiClusterRef struct {
	Name string `json:"name"`
}

// +kubebuilder:validation:Enum=Managed;AdoptionRefused;OwnershipConflict
type DataflowOwnershipState string

const (
	DataflowOwnershipStateManaged           DataflowOwnershipState = "Managed"
	DataflowOwnershipStateAdoptionRefused   DataflowOwnershipState = "AdoptionRefused"
	DataflowOwnershipStateOwnershipConflict DataflowOwnershipState = "OwnershipConflict"
)

type RegistryClientRef struct {
	Name string `json:"name"`
}

type ParameterContextRef struct {
	Name string `json:"name"`
}

type DataflowSource struct {
	RegistryClient RegistryClientRef `json:"registryClient"`
	Bucket         string            `json:"bucket"`
	Flow           string            `json:"flow"`
	Version        string            `json:"version"`
}

type DataflowTarget struct {
	RootChildProcessGroupName string               `json:"rootChildProcessGroupName"`
	ParameterContextRef       *ParameterContextRef `json:"parameterContextRef,omitempty"`
}

type DataflowRolloutPolicy struct {
	Strategy DataflowRolloutStrategy `json:"strategy,omitempty"`
	Timeout  metav1.Duration         `json:"timeout,omitempty"`
}

type DataflowSyncPolicy struct {
	Mode DataflowSyncMode `json:"mode,omitempty"`
}

type RetainedOwnedImportStatus struct {
	Name                       string `json:"name,omitempty"`
	TargetRootProcessGroupName string `json:"targetRootProcessGroupName,omitempty"`
	ProcessGroupID             string `json:"processGroupId,omitempty"`
	Reason                     string `json:"reason,omitempty"`
}

type DataflowWarningsStatus struct {
	RetainedOwnedImports []RetainedOwnedImportStatus `json:"retainedOwnedImports,omitempty"`
}

type DataflowOwnershipStatus struct {
	State   DataflowOwnershipState `json:"state,omitempty"`
	Reason  string                 `json:"reason,omitempty"`
	Message string                 `json:"message,omitempty"`
}

type NiFiDataflowSpec struct {
	ClusterRef NiFiClusterRef        `json:"clusterRef"`
	Source     DataflowSource        `json:"source"`
	Target     DataflowTarget        `json:"target"`
	Rollout    DataflowRolloutPolicy `json:"rollout,omitempty"`
	SyncPolicy DataflowSyncPolicy    `json:"syncPolicy,omitempty"`
	Suspend    bool                  `json:"suspend,omitempty"`
}

type NiFiDataflowStatus struct {
	ObservedGeneration    int64                   `json:"observedGeneration,omitempty"`
	Phase                 DataflowPhase           `json:"phase,omitempty"`
	ProcessGroupID        string                  `json:"processGroupId,omitempty"`
	ObservedVersion       string                  `json:"observedVersion,omitempty"`
	LastSuccessfulVersion string                  `json:"lastSuccessfulVersion,omitempty"`
	Warnings              DataflowWarningsStatus  `json:"warnings,omitempty"`
	Ownership             DataflowOwnershipStatus `json:"ownership,omitempty"`
	LastOperation         LastOperation           `json:"lastOperation,omitempty"`
	Conditions            []metav1.Condition      `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=nifidataflows,scope=Namespaced,categories=nifi
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".spec.clusterRef.name"
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".status.observedVersion"
// +kubebuilder:printcolumn:name="Ownership",type="string",JSONPath=".status.ownership.state"
// +kubebuilder:printcolumn:name="Retained",type="string",JSONPath=".status.warnings.retainedOwnedImports[0].name",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// NiFiDataflow is a bounded declarative deployment record for one imported versioned flow target.
type NiFiDataflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NiFiDataflowSpec   `json:"spec,omitempty"`
	Status NiFiDataflowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NiFiDataflowList contains a list of NiFiDataflow.
type NiFiDataflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NiFiDataflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NiFiDataflow{}, &NiFiDataflowList{})
}
