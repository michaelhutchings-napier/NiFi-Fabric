package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const nifiDataflowFinalizer = "platform.nifi.io/nifidataflow-finalizer"

type bridgeRuntimeStatus struct {
	Status               string                      `json:"status"`
	Reason               string                      `json:"reason"`
	Imports              []bridgeRuntimeImportRef    `json:"imports"`
	RetainedOwnedImports []bridgeRetainedOwnedImport `json:"retainedOwnedImports"`
}

type bridgeRuntimeImportRef struct {
	Name                 string `json:"name"`
	Status               string `json:"status"`
	Reason               string `json:"reason"`
	Action               string `json:"action"`
	OwnershipState       string `json:"ownershipState"`
	ProcessGroupID       string `json:"processGroupId"`
	RegistryClientName   string `json:"registryClientName"`
	RegistryClientID     string `json:"registryClientId"`
	Bucket               string `json:"bucket"`
	BucketID             string `json:"bucketId"`
	FlowName             string `json:"flowName"`
	FlowID               string `json:"flowId"`
	ResolvedVersion      string `json:"resolvedVersion"`
	ActualVersion        string `json:"actualVersion"`
	ParameterContextName string `json:"parameterContextName"`
	ParameterContextID   string `json:"parameterContextId"`
}

type bridgeRetainedOwnedImport struct {
	Name                       string `json:"name"`
	TargetRootProcessGroupName string `json:"targetRootProcessGroupName"`
	ProcessGroupID             string `json:"processGroupId"`
	Action                     string `json:"action"`
	Status                     string `json:"status"`
	Reason                     string `json:"reason"`
}

// NiFiDataflowReconciler keeps the initial dataflow API thin and status-focused.
// This slice publishes aggregated NiFiDataflow import declarations into a
// controller-owned ConfigMap that the existing in-pod bounded import runner can
// consume.
type NiFiDataflowReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	APIReader client.Reader
	Recorder  record.EventRecorder
}

func (r *NiFiDataflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	dataflow := &platformv1alpha1.NiFiDataflow{}
	if err := r.Get(ctx, req.NamespacedName, dataflow); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if dataflow.DeletionTimestamp.IsZero() {
		added, err := r.ensureFinalizer(ctx, dataflow)
		if err != nil {
			return ctrl.Result{}, err
		}
		if added {
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		removed, err := r.finalizeDataflow(ctx, dataflow)
		if err != nil {
			return ctrl.Result{}, err
		}
		if removed {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	original := dataflow.DeepCopy()
	dataflow.InitializeConditions()
	dataflow.Status.ObservedGeneration = dataflow.Generation
	dataflow.Status.Warnings = platformv1alpha1.DataflowWarningsStatus{}
	dataflow.Status.Ownership = platformv1alpha1.DataflowOwnershipStatus{}

	result, reconcileErr := r.reconcileDataflow(ctx, dataflow)
	if patchErr := r.patchStatus(ctx, original, dataflow); patchErr != nil {
		if reconcileErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile error: %v; patch status: %w", reconcileErr, patchErr)
		}
		return ctrl.Result{}, patchErr
	}
	r.emitStatusEventIfNeeded(original, dataflow)
	if reconcileErr != nil {
		return result, reconcileErr
	}

	return result, nil
}

func (r *NiFiDataflowReconciler) reconcileDataflow(ctx context.Context, dataflow *platformv1alpha1.NiFiDataflow) (ctrl.Result, error) {
	if dataflow.Spec.Suspend {
		r.markSuspended(dataflow)
		return ctrl.Result{}, nil
	}

	cluster := &platformv1alpha1.NiFiCluster{}
	clusterKey := types.NamespacedName{Namespace: dataflow.Namespace, Name: dataflow.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.markClusterMissing(dataflow, clusterKey.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get referenced NiFiCluster: %w", err)
	}

	if cluster.Spec.DesiredState != platformv1alpha1.DesiredStateRunning {
		r.markClusterNotRunning(dataflow, cluster)
		return ctrl.Result{}, nil
	}

	target := &appsv1.StatefulSet{}
	targetKey := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}
	if err := r.Get(ctx, targetKey, target); err != nil {
		if apierrors.IsNotFound(err) {
			r.markTargetStatefulSetMissing(dataflow, cluster, targetKey.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get target StatefulSet: %w", err)
	}

	configMapName, importCount, err := r.publishBridgeConfigMap(ctx, cluster, "")
	if err != nil {
		return ctrl.Result{}, err
	}

	if !statefulSetSupportsDataflowBridge(target, configMapName) {
		r.markBridgeUnsupported(dataflow, cluster, target, configMapName, importCount)
		return ctrl.Result{}, nil
	}

	statusConfigMapName := bridgeStatusConfigMapName(cluster.Spec.TargetRef.Name)
	runtimeStatus, err := r.readBridgeRuntimeStatus(ctx, cluster, statusConfigMapName)
	if err != nil {
		r.markRuntimeStatusUnreadable(dataflow, cluster, statusConfigMapName, err)
		return ctrl.Result{}, nil
	}

	if runtimeStatus != nil && runtimeStatus.Status == "failed" {
		r.markRuntimeExecutionFailed(dataflow, cluster, statusConfigMapName, runtimeStatus.Reason)
		return ctrl.Result{}, nil
	}

	if runtimeImport := findBridgeRuntimeImport(runtimeStatus, dataflow.Name); runtimeImport != nil {
		r.applyRuntimeImportStatus(dataflow, cluster, configMapName, statusConfigMapName, runtimeImport)
		r.applyRetainedOwnedImportWarnings(dataflow, runtimeStatus)
		return ctrl.Result{}, nil
	}

	r.markBridgePublished(dataflow, cluster, configMapName, importCount)
	r.applyRetainedOwnedImportWarnings(dataflow, runtimeStatus)
	return ctrl.Result{}, nil
}

func (r *NiFiDataflowReconciler) finalizeDataflow(ctx context.Context, dataflow *platformv1alpha1.NiFiDataflow) (bool, error) {
	if !controllerutil.ContainsFinalizer(dataflow, nifiDataflowFinalizer) {
		return false, nil
	}

	cluster := &platformv1alpha1.NiFiCluster{}
	clusterKey := types.NamespacedName{Namespace: dataflow.Namespace, Name: dataflow.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, cluster); err == nil {
		if _, _, publishErr := r.publishBridgeConfigMap(ctx, cluster, string(dataflow.UID)); publishErr != nil {
			return false, publishErr
		}
	} else if !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("get referenced NiFiCluster during finalization: %w", err)
	}

	patch := client.MergeFrom(dataflow.DeepCopy())
	controllerutil.RemoveFinalizer(dataflow, nifiDataflowFinalizer)
	if err := r.Patch(ctx, dataflow, patch); err != nil {
		return false, fmt.Errorf("remove finalizer: %w", err)
	}

	return true, nil
}

func (r *NiFiDataflowReconciler) ensureFinalizer(ctx context.Context, dataflow *platformv1alpha1.NiFiDataflow) (bool, error) {
	if controllerutil.ContainsFinalizer(dataflow, nifiDataflowFinalizer) {
		return false, nil
	}

	patch := client.MergeFrom(dataflow.DeepCopy())
	controllerutil.AddFinalizer(dataflow, nifiDataflowFinalizer)
	if err := r.Patch(ctx, dataflow, patch); err != nil {
		return false, fmt.Errorf("add finalizer: %w", err)
	}

	return true, nil
}

func (r *NiFiDataflowReconciler) publishBridgeConfigMap(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, excludeUID string) (string, int, error) {
	dataflows, err := r.activeDataflowsForCluster(ctx, cluster, excludeUID)
	if err != nil {
		return "", 0, err
	}

	configMapName := bridgeConfigMapName(cluster.Spec.TargetRef.Name)
	configMapKey := types.NamespacedName{Namespace: cluster.Namespace, Name: configMapName}
	if len(dataflows) == 0 {
		configMap := &corev1.ConfigMap{}
		if err := r.Get(ctx, configMapKey, configMap); err != nil {
			if apierrors.IsNotFound(err) {
				return configMapName, 0, nil
			}
			return "", 0, fmt.Errorf("get bridge ConfigMap for cleanup: %w", err)
		}
		if err := r.Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
			return "", 0, fmt.Errorf("delete empty bridge ConfigMap: %w", err)
		}
		return configMapName, 0, nil
	}

	importsData, err := bridgeImportsJSON(cluster, dataflows)
	if err != nil {
		return "", 0, err
	}

	configMap := &corev1.ConfigMap{}
	if err := r.Get(ctx, configMapKey, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return "", 0, fmt.Errorf("get bridge ConfigMap: %w", err)
		}

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "nifi-fabric-controller",
					"app.kubernetes.io/component":  "nifidataflow-bridge",
					"platform.nifi.io/cluster":     cluster.Name,
					"platform.nifi.io/target-ref":  cluster.Spec.TargetRef.Name,
				},
			},
			Data: map[string]string{
				"imports.json": importsData,
				"README.txt":   "Controller-owned NiFiDataflow bridge catalog consumed by the bounded versioned-flow import runtime bundle.\n",
			},
		}
		if err := controllerutil.SetControllerReference(cluster, configMap, r.Scheme); err != nil {
			return "", 0, fmt.Errorf("set bridge ConfigMap owner reference: %w", err)
		}
		if err := r.Create(ctx, configMap); err != nil {
			return "", 0, fmt.Errorf("create bridge ConfigMap: %w", err)
		}
		return configMapName, len(dataflows), nil
	}

	updated := configMap.DeepCopy()
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	updated.Labels["app.kubernetes.io/managed-by"] = "nifi-fabric-controller"
	updated.Labels["app.kubernetes.io/component"] = "nifidataflow-bridge"
	updated.Labels["platform.nifi.io/cluster"] = cluster.Name
	updated.Labels["platform.nifi.io/target-ref"] = cluster.Spec.TargetRef.Name
	if updated.Data == nil {
		updated.Data = map[string]string{}
	}
	updated.Data["imports.json"] = importsData
	updated.Data["README.txt"] = "Controller-owned NiFiDataflow bridge catalog consumed by the bounded versioned-flow import runtime bundle.\n"
	if err := controllerutil.SetControllerReference(cluster, updated, r.Scheme); err != nil {
		return "", 0, fmt.Errorf("set bridge ConfigMap owner reference: %w", err)
	}

	if equality.Semantic.DeepEqual(configMap.Labels, updated.Labels) && equality.Semantic.DeepEqual(configMap.Data, updated.Data) {
		return configMapName, len(dataflows), nil
	}
	if err := r.Patch(ctx, updated, client.MergeFrom(configMap)); err != nil {
		return "", 0, fmt.Errorf("patch bridge ConfigMap: %w", err)
	}

	return configMapName, len(dataflows), nil
}

func (r *NiFiDataflowReconciler) activeDataflowsForCluster(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, excludeUID string) ([]platformv1alpha1.NiFiDataflow, error) {
	dataflows := &platformv1alpha1.NiFiDataflowList{}
	if err := r.APIReader.List(ctx, dataflows, client.InNamespace(cluster.Namespace)); err != nil {
		return nil, fmt.Errorf("list NiFiDataflows: %w", err)
	}

	filtered := make([]platformv1alpha1.NiFiDataflow, 0, len(dataflows.Items))
	for _, dataflow := range dataflows.Items {
		if dataflow.Spec.ClusterRef.Name != cluster.Name {
			continue
		}
		if dataflow.DeletionTimestamp != nil {
			continue
		}
		if excludeUID != "" && string(dataflow.UID) == excludeUID {
			continue
		}
		filtered = append(filtered, dataflow)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})

	return filtered, nil
}

func bridgeImportsJSON(cluster *platformv1alpha1.NiFiCluster, dataflows []platformv1alpha1.NiFiDataflow) (string, error) {
	imports := make([]map[string]any, 0, len(dataflows))
	for _, dataflow := range dataflows {
		entry := map[string]any{
			"name":          dataflow.Name,
			"bootstrapMode": "runtime-managed",
			"driftPolicy": map[string]any{
				"createdByProduct":             true,
				"reconciledByProduct":          true,
				"deletedWhenRemovedFromValues": false,
				"manualNiFiDrift":              "bounded-live-reconcile-or-blocked",
			},
			"registryClientRef": map[string]string{
				"name": dataflow.Spec.Source.RegistryClient.Name,
			},
			"source": map[string]string{
				"bucket":   dataflow.Spec.Source.Bucket,
				"flowName": dataflow.Spec.Source.Flow,
				"version":  dataflow.Spec.Source.Version,
			},
			"target": map[string]string{
				"rootProcessGroupName": dataflow.Spec.Target.RootChildProcessGroupName,
			},
			"parameterContextRefs": []map[string]string{},
			"managedBy":            "NiFiDataflow",
			"clusterRef": map[string]string{
				"name": cluster.Name,
			},
		}
		if dataflow.Spec.Target.ParameterContextRef != nil {
			entry["parameterContextRefs"] = []map[string]string{{"name": dataflow.Spec.Target.ParameterContextRef.Name}}
		}
		imports = append(imports, entry)
	}

	payload := map[string]any{
		"apiVersion": "platform.nifi.io/v1alpha1",
		"kind":       "NiFiDataflowBridgeCatalog",
		"clusterRef": map[string]string{
			"name": cluster.Name,
		},
		"targetRef": map[string]string{
			"name": cluster.Spec.TargetRef.Name,
		},
		"imports": imports,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal bridge imports JSON: %w", err)
	}
	return string(body) + "\n", nil
}

func bridgeConfigMapName(targetName string) string {
	name := fmt.Sprintf("%s-nifidataflows", targetName)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func bridgeStatusConfigMapName(targetName string) string {
	name := fmt.Sprintf("%s-nifidataflows-status", targetName)
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.TrimRight(name, "-")
}

func statefulSetSupportsDataflowBridge(target *appsv1.StatefulSet, configMapName string) bool {
	for _, volume := range target.Spec.Template.Spec.Volumes {
		if volume.ConfigMap == nil {
			continue
		}
		if volume.ConfigMap.Name == configMapName {
			return true
		}
	}
	return false
}

func findBridgeRuntimeImport(runtimeStatus *bridgeRuntimeStatus, name string) *bridgeRuntimeImportRef {
	if runtimeStatus == nil {
		return nil
	}
	for i := range runtimeStatus.Imports {
		if runtimeStatus.Imports[i].Name == name {
			return &runtimeStatus.Imports[i]
		}
	}
	return nil
}

func runtimeImportObservedVersion(runtimeImport *bridgeRuntimeImportRef) string {
	if runtimeImport == nil {
		return ""
	}
	if version := strings.TrimSpace(runtimeImport.ActualVersion); version != "" {
		return version
	}
	return strings.TrimSpace(runtimeImport.ResolvedVersion)
}

func runtimeImportSummary(runtimeImport *bridgeRuntimeImportRef) string {
	if runtimeImport == nil {
		return ""
	}

	parts := make([]string, 0, 4)
	if processGroupID := strings.TrimSpace(runtimeImport.ProcessGroupID); processGroupID != "" {
		parts = append(parts, fmt.Sprintf("process group %s", processGroupID))
	}
	if observedVersion := runtimeImportObservedVersion(runtimeImport); observedVersion != "" {
		parts = append(parts, fmt.Sprintf("version %s", observedVersion))
	}
	if strings.TrimSpace(runtimeImport.RegistryClientName) != "" && strings.TrimSpace(runtimeImport.FlowName) != "" {
		parts = append(parts, fmt.Sprintf("registry client %s flow %s", runtimeImport.RegistryClientName, runtimeImport.FlowName))
	}
	if parameterContextName := strings.TrimSpace(runtimeImport.ParameterContextName); parameterContextName != "" {
		parts = append(parts, fmt.Sprintf("Parameter Context %s", parameterContextName))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func runtimeImportSourceResolved(runtimeImport *bridgeRuntimeImportRef) bool {
	if runtimeImport == nil {
		return false
	}
	return strings.TrimSpace(runtimeImport.RegistryClientID) != "" &&
		strings.TrimSpace(runtimeImport.BucketID) != "" &&
		strings.TrimSpace(runtimeImport.FlowID) != "" &&
		strings.TrimSpace(runtimeImport.ResolvedVersion) != ""
}

func (r *NiFiDataflowReconciler) readBridgeRuntimeStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, configMapName string) (*bridgeRuntimeStatus, error) {
	configMap := &corev1.ConfigMap{}
	configMapKey := types.NamespacedName{Namespace: cluster.Namespace, Name: configMapName}
	if err := r.Get(ctx, configMapKey, configMap); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get bridge status ConfigMap: %w", err)
	}

	raw := strings.TrimSpace(configMap.Data["status.json"])
	if raw == "" || raw == "{}" {
		return nil, nil
	}

	runtimeStatus := &bridgeRuntimeStatus{}
	if err := json.Unmarshal([]byte(raw), runtimeStatus); err != nil {
		return nil, fmt.Errorf("decode status.json from ConfigMap %s: %w", configMapName, err)
	}
	return runtimeStatus, nil
}

func (r *NiFiDataflowReconciler) patchStatus(ctx context.Context, original, updated *platformv1alpha1.NiFiDataflow) error {
	if original.Status.ObservedGeneration == updated.Status.ObservedGeneration &&
		original.Status.Phase == updated.Status.Phase &&
		original.Status.ProcessGroupID == updated.Status.ProcessGroupID &&
		original.Status.ObservedVersion == updated.Status.ObservedVersion &&
		original.Status.LastSuccessfulVersion == updated.Status.LastSuccessfulVersion &&
		equality.Semantic.DeepEqual(original.Status.Warnings, updated.Status.Warnings) &&
		equality.Semantic.DeepEqual(original.Status.Ownership, updated.Status.Ownership) &&
		original.Status.LastOperation == updated.Status.LastOperation &&
		equality.Semantic.DeepEqual(original.Status.Conditions, updated.Status.Conditions) {
		return nil
	}

	return r.Status().Patch(ctx, updated, client.MergeFrom(original))
}

func (r *NiFiDataflowReconciler) emitStatusEventIfNeeded(original, updated *platformv1alpha1.NiFiDataflow) {
	if r.Recorder == nil {
		return
	}
	if original == nil || updated == nil {
		return
	}
	if !shouldEmitStatusEvent(original, updated) {
		return
	}

	if eventType, reason, message, ok := transitionEventOverride(original, updated); ok {
		r.Recorder.Event(updated, eventType, reason, message)
		return
	}

	eventType, reason := statusEventMetadata(updated)
	message := statusEventMessage(updated)
	if message == "" {
		message = fmt.Sprintf("NiFiDataflow %s transitioned to phase %s", updated.Name, updated.Status.Phase)
	}
	r.Recorder.Event(updated, eventType, reason, message)
}

func shouldEmitStatusEvent(original, updated *platformv1alpha1.NiFiDataflow) bool {
	return statusEventSignature(original) != statusEventSignature(updated)
}

func statusEventMetadata(dataflow *platformv1alpha1.NiFiDataflow) (string, string) {
	if dataflow == nil {
		return corev1.EventTypeNormal, "StatusUpdated"
	}
	if condition := dataflow.GetCondition(platformv1alpha1.ConditionDegraded); condition != nil && condition.Reason == "RetainedOwnedImportsPresent" {
		return corev1.EventTypeWarning, "RetainedOwnedImportsPresent"
	}
	if condition := dataflow.GetCondition(platformv1alpha1.ConditionTargetResolved); condition != nil {
		switch condition.Reason {
		case "AdoptionRefused":
			return corev1.EventTypeWarning, "AdoptionRefused"
		case "OwnershipConflict":
			return corev1.EventTypeWarning, "OwnershipConflict"
		}
	}

	switch dataflow.Status.Phase {
	case platformv1alpha1.DataflowPhaseReady:
		return corev1.EventTypeNormal, "RuntimeImportReady"
	case platformv1alpha1.DataflowPhaseProgressing, platformv1alpha1.DataflowPhaseImporting:
		return corev1.EventTypeNormal, "RuntimeImportProgressing"
	case platformv1alpha1.DataflowPhaseBlocked:
		return corev1.EventTypeWarning, "RuntimeImportBlocked"
	case platformv1alpha1.DataflowPhaseFailed:
		return corev1.EventTypeWarning, "RuntimeImportFailed"
	default:
		return corev1.EventTypeNormal, "StatusUpdated"
	}
}

func transitionEventOverride(original, updated *platformv1alpha1.NiFiDataflow) (string, string, string, bool) {
	if retainedOwnedImportsWarningCleared(original, updated) {
		return corev1.EventTypeNormal, "RetainedOwnedImportsCleared", fmt.Sprintf("NiFiDataflow %s warning cleared: no retained owned imports remain", updated.Name), true
	}
	return "", "", "", false
}

func retainedOwnedImportsWarningCleared(original, updated *platformv1alpha1.NiFiDataflow) bool {
	if original == nil || updated == nil {
		return false
	}
	originalCondition := original.GetCondition(platformv1alpha1.ConditionDegraded)
	if originalCondition == nil || originalCondition.Reason != "RetainedOwnedImportsPresent" || originalCondition.Status != metav1.ConditionTrue {
		return false
	}
	updatedCondition := updated.GetCondition(platformv1alpha1.ConditionDegraded)
	return updatedCondition == nil || updatedCondition.Reason != "RetainedOwnedImportsPresent" || updatedCondition.Status != metav1.ConditionTrue
}

func statusEventSignature(dataflow *platformv1alpha1.NiFiDataflow) string {
	if dataflow == nil {
		return ""
	}

	eventType, eventReason := statusEventMetadata(dataflow)
	signatureParts := []string{
		eventType,
		eventReason,
		string(dataflow.Status.Phase),
		dataflow.Status.LastOperation.Type,
		string(dataflow.Status.LastOperation.Phase),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionTargetResolved)),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionSourceResolved)),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionParameterContextReady)),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionAvailable)),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionProgressing)),
		conditionSignaturePart(dataflow.GetCondition(platformv1alpha1.ConditionDegraded)),
	}

	switch dataflow.Status.Phase {
	case platformv1alpha1.DataflowPhaseReady:
		signatureParts = append(signatureParts, dataflow.Status.ProcessGroupID, dataflow.Status.ObservedVersion, dataflow.Status.LastSuccessfulVersion)
	case platformv1alpha1.DataflowPhaseProgressing, platformv1alpha1.DataflowPhaseImporting:
		signatureParts = append(signatureParts, dataflow.Status.ProcessGroupID, dataflow.Status.ObservedVersion)
	}

	return strings.Join(signatureParts, "|")
}

func conditionSignaturePart(condition *metav1.Condition) string {
	if condition == nil {
		return ""
	}
	parts := []string{condition.Type, string(condition.Status), condition.Reason}
	if condition.Reason == "RetainedOwnedImportsPresent" {
		parts = append(parts, condition.Message)
	}
	return strings.Join(parts, ":")
}

func statusEventMessage(dataflow *platformv1alpha1.NiFiDataflow) string {
	if dataflow == nil {
		return ""
	}
	if condition := dataflow.GetCondition(platformv1alpha1.ConditionDegraded); condition != nil && condition.Reason == "RetainedOwnedImportsPresent" {
		return fmt.Sprintf("NiFiDataflow %s warning: %s", dataflow.Name, condition.Message)
	}

	switch dataflow.Status.Phase {
	case platformv1alpha1.DataflowPhaseReady:
		parts := []string{fmt.Sprintf("NiFiDataflow %s is Ready", dataflow.Name)}
		if processGroupID := strings.TrimSpace(dataflow.Status.ProcessGroupID); processGroupID != "" {
			parts = append(parts, fmt.Sprintf("process group %s", processGroupID))
		}
		if observedVersion := strings.TrimSpace(dataflow.Status.ObservedVersion); observedVersion != "" {
			parts = append(parts, fmt.Sprintf("version %s", observedVersion))
		}
		return strings.Join(parts, ": ")
	case platformv1alpha1.DataflowPhaseProgressing, platformv1alpha1.DataflowPhaseImporting:
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionProgressing); condition != nil && condition.Reason == "BridgePublished" {
			return fmt.Sprintf("NiFiDataflow %s is Progressing: controller bridge is published and waiting for bounded runtime reconcile", dataflow.Name)
		}
		if observedVersion := strings.TrimSpace(dataflow.Status.ObservedVersion); observedVersion != "" {
			return fmt.Sprintf("NiFiDataflow %s is Progressing: bounded runtime reconcile is in progress for version %s", dataflow.Name, observedVersion)
		}
		return fmt.Sprintf("NiFiDataflow %s is Progressing: bounded runtime reconcile is in progress", dataflow.Name)
	case platformv1alpha1.DataflowPhaseBlocked:
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionTargetResolved); condition != nil && condition.Reason == "AdoptionRefused" {
			return fmt.Sprintf("NiFiDataflow %s is Blocked: existing target is not owned by this resource and will not be adopted automatically", dataflow.Name)
		}
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionTargetResolved); condition != nil && condition.Reason == "OwnershipConflict" {
			return fmt.Sprintf("NiFiDataflow %s is Blocked: existing owned target conflicts with this declaration and must be deleted or renamed before ownership changes", dataflow.Name)
		}
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionParameterContextReady); condition != nil && condition.Status == metav1.ConditionFalse {
			if observedVersion := strings.TrimSpace(dataflow.Status.ObservedVersion); observedVersion != "" {
				return fmt.Sprintf("NiFiDataflow %s is Blocked: bounded runtime import is blocked on Parameter Context attachment for version %s", dataflow.Name, observedVersion)
			}
			return fmt.Sprintf("NiFiDataflow %s is Blocked: bounded runtime import is blocked on Parameter Context attachment", dataflow.Name)
		}
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionSourceResolved); condition != nil && condition.Status == metav1.ConditionFalse {
			return fmt.Sprintf("NiFiDataflow %s is Blocked: bounded runtime import is blocked before source resolution completed", dataflow.Name)
		}
		if condition := dataflow.GetCondition(platformv1alpha1.ConditionTargetResolved); condition != nil && condition.Status == metav1.ConditionFalse {
			return fmt.Sprintf("NiFiDataflow %s is Blocked: target wiring or referenced resources are not ready", dataflow.Name)
		}
		return fmt.Sprintf("NiFiDataflow %s is Blocked: bounded runtime import needs operator attention", dataflow.Name)
	case platformv1alpha1.DataflowPhaseFailed:
		return fmt.Sprintf("NiFiDataflow %s is Failed: bounded runtime status observation needs operator attention", dataflow.Name)
	default:
		return strings.TrimSpace(dataflow.Status.LastOperation.Message)
	}
}

func retainedOwnedImportWarning(runtimeStatus *bridgeRuntimeStatus) string {
	if runtimeStatus == nil || len(runtimeStatus.RetainedOwnedImports) == 0 {
		return ""
	}

	summaries := make([]string, 0, len(runtimeStatus.RetainedOwnedImports))
	for _, entry := range runtimeStatus.RetainedOwnedImports {
		name := strings.TrimSpace(entry.Name)
		target := strings.TrimSpace(entry.TargetRootProcessGroupName)
		processGroupID := strings.TrimSpace(entry.ProcessGroupID)

		summary := name
		if summary == "" {
			summary = target
		}
		if summary == "" {
			summary = processGroupID
		}
		if summary == "" {
			continue
		}

		if target != "" && target != summary {
			summary = fmt.Sprintf("%s (target %s)", summary, target)
		} else if processGroupID != "" && processGroupID != summary {
			summary = fmt.Sprintf("%s (process group %s)", summary, processGroupID)
		}
		summaries = append(summaries, summary)
	}

	if len(summaries) == 0 {
		return ""
	}

	sort.Strings(summaries)
	return fmt.Sprintf("bounded runtime reports retained owned imports no longer declared: %s", strings.Join(summaries, ", "))
}

func retainedOwnedImportStatuses(runtimeStatus *bridgeRuntimeStatus) []platformv1alpha1.RetainedOwnedImportStatus {
	if runtimeStatus == nil || len(runtimeStatus.RetainedOwnedImports) == 0 {
		return nil
	}

	statuses := make([]platformv1alpha1.RetainedOwnedImportStatus, 0, len(runtimeStatus.RetainedOwnedImports))
	for _, entry := range runtimeStatus.RetainedOwnedImports {
		statuses = append(statuses, platformv1alpha1.RetainedOwnedImportStatus{
			Name:                       strings.TrimSpace(entry.Name),
			TargetRootProcessGroupName: strings.TrimSpace(entry.TargetRootProcessGroupName),
			ProcessGroupID:             strings.TrimSpace(entry.ProcessGroupID),
			Reason:                     strings.TrimSpace(entry.Reason),
		})
	}

	sort.Slice(statuses, func(i, j int) bool {
		left := statuses[i].Name
		if left == "" {
			left = statuses[i].TargetRootProcessGroupName
		}
		right := statuses[j].Name
		if right == "" {
			right = statuses[j].TargetRootProcessGroupName
		}
		return left < right
	})
	return statuses
}

func (r *NiFiDataflowReconciler) applyRetainedOwnedImportWarnings(dataflow *platformv1alpha1.NiFiDataflow, runtimeStatus *bridgeRuntimeStatus) {
	if dataflow == nil {
		return
	}
	dataflow.Status.Warnings.RetainedOwnedImports = retainedOwnedImportStatuses(runtimeStatus)
	if dataflow.Status.Phase != platformv1alpha1.DataflowPhaseReady && dataflow.Status.Phase != platformv1alpha1.DataflowPhaseProgressing {
		return
	}

	warningMessage := retainedOwnedImportWarning(runtimeStatus)
	if warningMessage == "" {
		return
	}

	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "RetainedOwnedImportsPresent",
		Message:            warningMessage,
		LastTransitionTime: metav1.Now(),
	})
}

func classifyOwnershipBlockedReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	switch {
	case strings.Contains(trimmed, "operator-owned targets are not adopted automatically"):
		return "AdoptionRefused"
	case strings.Contains(trimmed, "is already owned by import"), strings.Contains(trimmed, "delete or rename the owned target before changing the declared source or target"):
		return "OwnershipConflict"
	default:
		return ""
	}
}

func setOwnershipStatus(dataflow *platformv1alpha1.NiFiDataflow, state platformv1alpha1.DataflowOwnershipState, reason, message string) {
	if dataflow == nil {
		return
	}
	dataflow.Status.Ownership = platformv1alpha1.DataflowOwnershipStatus{
		State:   state,
		Reason:  strings.TrimSpace(reason),
		Message: strings.TrimSpace(message),
	}
}

func (r *NiFiDataflowReconciler) markSuspended(dataflow *platformv1alpha1.NiFiDataflow) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhasePending
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: "Reconciliation is suspended by spec.suspend=true",
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Source resolution is paused while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Cluster resolution is paused while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionParameterContextReady,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Parameter Context evaluation is paused while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            "No bridge publication work is running while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            "The flow bridge is not being reconciled while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            "Suspension is operator-requested, not a degraded state",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markClusterMissing(dataflow *platformv1alpha1.NiFiDataflow, clusterName string) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseBlocked
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: fmt.Sprintf("Referenced NiFiCluster %s was not found", clusterName),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionFalse,
		Reason:             "ClusterNotFound",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s does not exist", clusterName),
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetBlocked",
		Message:            "Source resolution is paused until the referenced NiFiCluster exists",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionParameterContextReady,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetBlocked",
		Message:            "Parameter Context evaluation is paused until the referenced NiFiCluster exists",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The controller cannot publish bridge configuration until the referenced NiFiCluster exists",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "No flow bridge can be published while the referenced NiFiCluster is missing",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The resource is blocked on a missing referenced cluster",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markClusterNotRunning(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseBlocked
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: fmt.Sprintf("Referenced NiFiCluster %s is not in desiredState=Running", cluster.Name),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "ClusterFound",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s exists", cluster.Name),
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "ClusterNotRunning",
		Message:            "Source resolution is paused until the referenced NiFiCluster is running",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionParameterContextReady,
		Status:             metav1.ConditionUnknown,
		Reason:             "ClusterNotRunning",
		Message:            "Parameter Context evaluation is paused until the referenced NiFiCluster is running",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The controller cannot publish bridge configuration while the referenced NiFiCluster is not running",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The declared flow cannot be bridged while the referenced NiFiCluster is not running",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The resource is blocked on a non-running referenced cluster",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markTargetStatefulSetMissing(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, targetName string) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseBlocked
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: fmt.Sprintf("Target StatefulSet %s for referenced NiFiCluster %s was not found", targetName, cluster.Name),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionFalse,
		Reason:             "StatefulSetNotFound",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s points to missing StatefulSet %s", cluster.Name, targetName),
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetBlocked",
		Message:            "Source resolution is paused until the target StatefulSet exists",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionParameterContextReady,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetBlocked",
		Message:            "Parameter Context evaluation is paused until the target StatefulSet exists",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The controller cannot publish a usable bridge while the target StatefulSet is missing",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The bounded import runtime bridge is unavailable while the target StatefulSet is missing",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Blocked",
		Message:            "The resource is blocked on a missing target StatefulSet",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markBridgeUnsupported(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, configMapName string, importCount int) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseBlocked
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhaseSucceeded,
		Message: fmt.Sprintf("Published %d bridged import declarations to ConfigMap %s, but target StatefulSet %s is not wired for the NiFiDataflow bridge", importCount, configMapName, target.Name),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionFalse,
		Reason:             "BridgeNotMounted",
		Message:            fmt.Sprintf("Target StatefulSet %s does not mount controller bridge ConfigMap %s", target.Name, configMapName),
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "WaitingForRuntimeBridge",
		Message:            "Live source resolution is delegated to the in-pod bounded import runner after the controller bridge is mounted",
		LastTransitionTime: metav1.Now(),
	})
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: metav1.Now(),
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "WaitingForRuntimeBridge",
			Message:            "Direct Parameter Context attachment will be resolved by the in-pod bounded import runner once the bridge is mounted",
			LastTransitionTime: metav1.Now(),
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "BridgeNotMounted",
		Message:            "The controller published bridge configuration, but the target workload is not wired to consume it yet",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "BridgeNotMounted",
		Message:            "The target workload must enable versionedFlowImports.controllerBridge.enabled to consume NiFiDataflow declarations",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "BridgeNotMounted",
		Message:            "The resource is blocked on workload wiring, not a failed live import operation",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markBridgePublished(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName string, importCount int) {
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseProgressing
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "PublishBridgeConfig",
		Phase:   platformv1alpha1.OperationPhaseSucceeded,
		Message: fmt.Sprintf("Published %d bridged import declarations to ConfigMap %s for NiFiCluster %s", importCount, configMapName, cluster.Name),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMap %s", cluster.Name, configMapName),
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "WaitingForRuntimeBridge",
		Message:            "Live source resolution is delegated to the in-pod bounded import runner after bridge publication",
		LastTransitionTime: metav1.Now(),
	})
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: metav1.Now(),
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "WaitingForRuntimeBridge",
			Message:            "Direct Parameter Context attachment will be resolved by the in-pod bounded import runner",
			LastTransitionTime: metav1.Now(),
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "BridgePublished",
		Message:            "The controller published bridge configuration and is waiting for the bounded in-pod import runner to apply it",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForRuntimeBridge",
		Message:            "The controller bridge is published, but live flow status is not yet observed back into NiFiDataflow status",
		LastTransitionTime: metav1.Now(),
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "BridgePublished",
		Message:            "No controller-side publication failure is present",
		LastTransitionTime: metav1.Now(),
	})
}

func (r *NiFiDataflowReconciler) markRuntimeStatusUnreadable(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName string, statusErr error) {
	now := metav1.Now()
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseFailed
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:        "ObserveRuntimeStatus",
		Phase:       platformv1alpha1.OperationPhaseFailed,
		CompletedAt: &now,
		Message:     fmt.Sprintf("Could not decode runtime bridge status ConfigMap %s for NiFiCluster %s: %v", configMapName, cluster.Name, statusErr),
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMaps", cluster.Name),
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "RuntimeStatusUnreadable",
		Message:            "The controller could not decode the bounded runtime status payload yet",
		LastTransitionTime: now,
	})
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "RuntimeStatusUnreadable",
			Message:            "The controller could not determine Parameter Context attachment state from the runtime status payload",
			LastTransitionTime: now,
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeStatusUnreadable",
		Message:            "The controller cannot project live import status while the runtime status payload is unreadable",
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeStatusUnreadable",
		Message:            "Live runtime import outcomes are not currently observable back into NiFiDataflow status",
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeStatusUnreadable",
		Message:            "The bounded runtime status payload needs operator attention before live import outcomes can be trusted",
		LastTransitionTime: now,
	})
}

func (r *NiFiDataflowReconciler) markRuntimeExecutionFailed(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName, reason string) {
	now := metav1.Now()
	if strings.TrimSpace(reason) == "" {
		reason = fmt.Sprintf("Runtime bridge status ConfigMap %s reported failure before a per-import result was available", configMapName)
	}
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseFailed
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:        "ObserveRuntimeStatus",
		Phase:       platformv1alpha1.OperationPhaseFailed,
		CompletedAt: &now,
		Message:     reason,
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMaps", cluster.Name),
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "RuntimeExecutionFailed",
		Message:            "The bounded runtime did not complete source resolution for this import",
		LastTransitionTime: now,
	})
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "RuntimeExecutionFailed",
			Message:            "The bounded runtime failed before direct Parameter Context attachment state was confirmed",
			LastTransitionTime: now,
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeExecutionFailed",
		Message:            "The bounded runtime is not currently making progress for this import",
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeExecutionFailed",
		Message:            reason,
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeExecutionFailed",
		Message:            "The bounded runtime reported a failure that needs operator attention",
		LastTransitionTime: now,
	})
}

func (r *NiFiDataflowReconciler) applyRuntimeImportStatus(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName, statusConfigMapName string, runtimeImport *bridgeRuntimeImportRef) {
	switch runtimeImport.Status {
	case "ok":
		r.markRuntimeImportReady(dataflow, cluster, configMapName, statusConfigMapName, runtimeImport)
	case "blocked":
		r.markRuntimeImportBlocked(dataflow, cluster, statusConfigMapName, runtimeImport)
	default:
		r.markRuntimeImportProgressing(dataflow, cluster, configMapName, statusConfigMapName, runtimeImport)
	}
}

func (r *NiFiDataflowReconciler) markRuntimeImportProgressing(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName, statusConfigMapName string, runtimeImport *bridgeRuntimeImportRef) {
	now := metav1.Now()
	message := fmt.Sprintf("Published controller bridge ConfigMap %s and observed runtime status %q for import %s in ConfigMap %s", configMapName, runtimeImport.Status, dataflow.Name, statusConfigMapName)
	if summary := runtimeImportSummary(runtimeImport); summary != "" {
		message = fmt.Sprintf("%s with %s", message, summary)
	}
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseProgressing
	if processGroupID := strings.TrimSpace(runtimeImport.ProcessGroupID); processGroupID != "" {
		dataflow.Status.ProcessGroupID = processGroupID
	}
	if observedVersion := runtimeImportObservedVersion(runtimeImport); observedVersion != "" {
		dataflow.Status.ObservedVersion = observedVersion
	}
	if strings.TrimSpace(runtimeImport.OwnershipState) == "owned" {
		setOwnershipStatus(dataflow, platformv1alpha1.DataflowOwnershipStateManaged, "OwnedTargetTracked", "The bounded runtime is reconciling an existing owned target")
	}
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:        "ObserveRuntimeStatus",
		Phase:       platformv1alpha1.OperationPhaseRunning,
		CompletedAt: &now,
		Message:     message,
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMap %s", cluster.Name, configMapName),
		LastTransitionTime: now,
	})
	if runtimeImportSourceResolved(runtimeImport) {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionSourceResolved,
			Status:             metav1.ConditionTrue,
			Reason:             "RuntimeSourceResolved",
			Message:            fmt.Sprintf("The bounded runtime resolved registry client %s, bucket %s, flow %s, and version %s", runtimeImport.RegistryClientName, runtimeImport.Bucket, runtimeImport.FlowName, runtimeImport.ResolvedVersion),
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionSourceResolved,
			Status:             metav1.ConditionUnknown,
			Reason:             "RuntimeProgressing",
			Message:            "The bounded runtime has not finished resolving the declared source yet",
			LastTransitionTime: now,
		})
	}
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: now,
		})
	} else if strings.TrimSpace(runtimeImport.ParameterContextID) != "" {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "RuntimeParameterContextAttached",
			Message:            fmt.Sprintf("The bounded runtime attached declared Parameter Context %s", runtimeImport.ParameterContextName),
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "RuntimeProgressing",
			Message:            "The bounded runtime has not finished evaluating the declared Parameter Context attachment yet",
			LastTransitionTime: now,
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeProgressing",
		Message:            message,
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeProgressing",
		Message:            message,
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeProgressing",
		Message:            "The bounded runtime is still reconciling the declared import",
		LastTransitionTime: now,
	})
}

func (r *NiFiDataflowReconciler) markRuntimeImportReady(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, configMapName, statusConfigMapName string, runtimeImport *bridgeRuntimeImportRef) {
	now := metav1.Now()
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseReady
	if processGroupID := strings.TrimSpace(runtimeImport.ProcessGroupID); processGroupID != "" {
		dataflow.Status.ProcessGroupID = processGroupID
	}
	if observedVersion := runtimeImportObservedVersion(runtimeImport); observedVersion != "" {
		dataflow.Status.ObservedVersion = observedVersion
		dataflow.Status.LastSuccessfulVersion = observedVersion
	}
	action := strings.TrimSpace(runtimeImport.Action)
	if action == "" {
		action = "reconciled"
	}
	message := fmt.Sprintf("Bounded runtime reported %s import %s through ConfigMap %s", action, dataflow.Name, statusConfigMapName)
	if summary := runtimeImportSummary(runtimeImport); summary != "" {
		message = fmt.Sprintf("%s with %s", message, summary)
	}
	setOwnershipStatus(dataflow, platformv1alpha1.DataflowOwnershipStateManaged, "OwnedTargetReconciled", "The bounded runtime reconciled the owned target for this NiFiDataflow")
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:        "ObserveRuntimeStatus",
		Phase:       platformv1alpha1.OperationPhaseSucceeded,
		CompletedAt: &now,
		Message:     message,
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMap %s", cluster.Name, configMapName),
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionSourceResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeSourceResolved",
		Message:            fmt.Sprintf("The bounded runtime resolved registry client %s, bucket %s, flow %s, and version %s", runtimeImport.RegistryClientName, runtimeImport.Bucket, runtimeImport.FlowName, runtimeImport.ResolvedVersion),
		LastTransitionTime: now,
	})
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: now,
		})
	} else if strings.TrimSpace(runtimeImport.ParameterContextID) != "" {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "RuntimeParameterContextAttached",
			Message:            fmt.Sprintf("The bounded runtime attached declared Parameter Context %s", runtimeImport.ParameterContextName),
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionFalse,
			Reason:             "RuntimeParameterContextMissing",
			Message:            "The bounded runtime completed the import but did not report the declared Parameter Context attachment",
			LastTransitionTime: now,
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeReady",
		Message:            "The bounded runtime is not currently reconciling further changes for this import",
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeReady",
		Message:            message,
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeReady",
		Message:            "No bounded runtime import failure is currently present",
		LastTransitionTime: now,
	})
}

func (r *NiFiDataflowReconciler) markRuntimeImportBlocked(dataflow *platformv1alpha1.NiFiDataflow, cluster *platformv1alpha1.NiFiCluster, statusConfigMapName string, runtimeImport *bridgeRuntimeImportRef) {
	now := metav1.Now()
	if processGroupID := strings.TrimSpace(runtimeImport.ProcessGroupID); processGroupID != "" {
		dataflow.Status.ProcessGroupID = processGroupID
	}
	if observedVersion := runtimeImportObservedVersion(runtimeImport); observedVersion != "" {
		dataflow.Status.ObservedVersion = observedVersion
	}
	reason := strings.TrimSpace(runtimeImport.Reason)
	if reason == "" {
		reason = fmt.Sprintf("The bounded runtime reported blocked status for import %s in ConfigMap %s", dataflow.Name, statusConfigMapName)
	}
	if summary := runtimeImportSummary(runtimeImport); summary != "" {
		reason = fmt.Sprintf("%s (%s)", reason, summary)
	}
	ownershipBlockedReason := classifyOwnershipBlockedReason(reason)
	dataflow.Status.Phase = platformv1alpha1.DataflowPhaseBlocked
	dataflow.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:        "ObserveRuntimeStatus",
		Phase:       platformv1alpha1.OperationPhaseFailed,
		CompletedAt: &now,
		Message:     reason,
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeBridgeMounted",
		Message:            fmt.Sprintf("Referenced NiFiCluster %s is running and its target StatefulSet is wired to consume controller bridge ConfigMaps", cluster.Name),
		LastTransitionTime: now,
	})
	if ownershipBlockedReason != "" {
		targetMessage := "The bounded runtime reported an ownership policy conflict for the declared target"
		if ownershipBlockedReason == "AdoptionRefused" {
			targetMessage = "The bounded runtime found an existing target without the product ownership marker and refused automatic adoption"
		}
		ownershipMessage := "The declared target conflicts with the current ownership metadata and cannot be adopted automatically"
		if ownershipBlockedReason == "AdoptionRefused" {
			ownershipMessage = "An existing target without the product ownership marker was found, and automatic adoption is intentionally refused"
		}
		setOwnershipStatus(dataflow, platformv1alpha1.DataflowOwnershipState(ownershipBlockedReason), ownershipBlockedReason, ownershipMessage)
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionTargetResolved,
			Status:             metav1.ConditionFalse,
			Reason:             ownershipBlockedReason,
			Message:            targetMessage,
			LastTransitionTime: now,
		})
	}
	if runtimeImportSourceResolved(runtimeImport) {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionSourceResolved,
			Status:             metav1.ConditionTrue,
			Reason:             "RuntimeSourceResolved",
			Message:            fmt.Sprintf("The bounded runtime resolved registry client %s, bucket %s, flow %s, and version %s before the import blocked", runtimeImport.RegistryClientName, runtimeImport.Bucket, runtimeImport.FlowName, runtimeImport.ResolvedVersion),
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionSourceResolved,
			Status:             metav1.ConditionFalse,
			Reason:             "RuntimeImportBlocked",
			Message:            "The bounded runtime blocked before the declared source was fully resolved",
			LastTransitionTime: now,
		})
	}
	if dataflow.Spec.Target.ParameterContextRef == nil {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "NotRequested",
			Message:            "No direct Parameter Context attachment was requested",
			LastTransitionTime: now,
		})
	} else if strings.TrimSpace(runtimeImport.ParameterContextID) != "" {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionTrue,
			Reason:             "RuntimeParameterContextAttached",
			Message:            fmt.Sprintf("The bounded runtime attached declared Parameter Context %s before blocking on a later step", runtimeImport.ParameterContextName),
			LastTransitionTime: now,
		})
	} else {
		dataflow.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionParameterContextReady,
			Status:             metav1.ConditionFalse,
			Reason:             "RuntimeImportBlocked",
			Message:            reason,
			LastTransitionTime: now,
		})
	}
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeImportBlocked",
		Message:            "The bounded runtime is not currently making progress for this import",
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RuntimeImportBlocked",
		Message:            reason,
		LastTransitionTime: now,
	})
	dataflow.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "RuntimeImportBlocked",
		Message:            "The bounded runtime reported a blocked import that needs operator attention",
		LastTransitionTime: now,
	})
}

func (r *NiFiDataflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("nifidataflow-controller")
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NiFiDataflow{}).
		Watches(&platformv1alpha1.NiFiCluster{}, handler.EnqueueRequestsFromMapFunc(r.requestsForNiFiCluster)).
		Watches(&appsv1.StatefulSet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForStatefulSet)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.requestsForConfigMap)).
		Complete(r)
}

func (r *NiFiDataflowReconciler) requestsForNiFiCluster(ctx context.Context, obj client.Object) []ctrl.Request {
	cluster, ok := obj.(*platformv1alpha1.NiFiCluster)
	if !ok {
		return nil
	}
	return r.requestsForClusterName(ctx, cluster.Namespace, cluster.Name)
}

func (r *NiFiDataflowReconciler) requestsForStatefulSet(ctx context.Context, obj client.Object) []ctrl.Request {
	statefulSet, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return nil
	}

	clusters := &platformv1alpha1.NiFiClusterList{}
	if err := r.APIReader.List(ctx, clusters, client.InNamespace(statefulSet.Namespace)); err != nil {
		return nil
	}

	requests := make([]ctrl.Request, 0)
	for _, cluster := range clusters.Items {
		if cluster.Spec.TargetRef.Name != statefulSet.Name {
			continue
		}
		requests = append(requests, r.requestsForClusterName(ctx, cluster.Namespace, cluster.Name)...)
	}
	return requests
}

func (r *NiFiDataflowReconciler) requestsForConfigMap(ctx context.Context, obj client.Object) []ctrl.Request {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}

	clusters := &platformv1alpha1.NiFiClusterList{}
	if err := r.APIReader.List(ctx, clusters, client.InNamespace(configMap.Namespace)); err != nil {
		return nil
	}

	requests := make([]ctrl.Request, 0)
	for _, cluster := range clusters.Items {
		targetName := cluster.Spec.TargetRef.Name
		if configMap.Name != bridgeConfigMapName(targetName) && configMap.Name != bridgeStatusConfigMapName(targetName) {
			continue
		}
		requests = append(requests, r.requestsForClusterName(ctx, cluster.Namespace, cluster.Name)...)
	}
	return requests
}

func (r *NiFiDataflowReconciler) requestsForClusterName(ctx context.Context, namespace, clusterName string) []ctrl.Request {
	dataflows := &platformv1alpha1.NiFiDataflowList{}
	if err := r.APIReader.List(ctx, dataflows, client.InNamespace(namespace)); err != nil {
		return nil
	}

	requests := make([]ctrl.Request, 0, len(dataflows.Items))
	for _, dataflow := range dataflows.Items {
		if dataflow.Spec.ClusterRef.Name != clusterName {
			continue
		}
		requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&dataflow)})
	}

	return requests
}
