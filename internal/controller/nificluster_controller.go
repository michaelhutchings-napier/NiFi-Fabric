package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
	"github.com/michaelhutchings-napier/nifi-made-simple/internal/nifi"
)

const rolloutPollRequeue = 5 * time.Second

// NiFiClusterReconciler keeps the operational API thin.
// Managed mode only adds health-gated OnDelete rollout sequencing.
type NiFiClusterReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	HealthChecker ClusterHealthChecker
}

func (r *NiFiClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cluster := &platformv1alpha1.NiFiCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if r.HealthChecker == nil {
		r.HealthChecker = &LiveClusterHealthChecker{
			KubeClient: r.Client,
			NiFiClient: nifi.NewHTTPClient(),
		}
	}

	original := cluster.DeepCopy()
	cluster.InitializeConditions()
	cluster.Status.ObservedGeneration = cluster.Generation

	result, reconcileErr := r.reconcileCluster(ctx, cluster)
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}

	logger.V(1).Info("reconciled NiFiCluster", "target", cluster.Spec.TargetRef.Name, "result", result)
	return r.patchStatus(ctx, original, cluster, result)
}

func (r *NiFiClusterReconciler) reconcileCluster(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) (ctrl.Result, error) {
	if cluster.Spec.Suspend {
		r.markSuspended(cluster)
		return ctrl.Result{}, nil
	}

	target := &appsv1.StatefulSet{}
	targetKey := client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}
	if err := r.Get(ctx, targetKey, target); err != nil {
		if apierrors.IsNotFound(err) {
			r.markTargetMissing(cluster, targetKey.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get target statefulset: %w", err)
	}

	pods, err := r.listTargetPods(ctx, target)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.syncReplicaStatus(cluster, target, pods)

	if !isManagedStatefulSet(target) {
		r.markUnmanagedTarget(cluster, target)
		return ctrl.Result{}, nil
	}

	if cluster.Spec.DesiredState != platformv1alpha1.DesiredStateRunning {
		r.markUnsupportedDesiredState(cluster)
		return ctrl.Result{}, nil
	}

	plan := BuildRolloutPlan(target, pods)
	if !plan.HasDrift() {
		return r.finishSteadyState(ctx, cluster, target)
	}

	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "RevisionDriftDetected",
		Message:            rolloutMessage(plan),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutInProgress",
		Message:            "Waiting for StatefulSet pods and NiFi cluster health to converge to the update revision",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutPending",
		Message:            "No rollout failure is currently active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("Rollout", fmt.Sprintf("Reconciling revision drift from %q to %q", plan.CurrentRevision, plan.UpdateRevision))

	if err := r.HealthChecker.WaitForPodsReady(ctx, target, podReadyTimeout(cluster)); err != nil {
		r.markHealthGateBlocked(cluster, fmt.Sprintf("Waiting for target pods to become Ready before deleting the next pod: %v", err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	healthResult, err := r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		r.markHealthGateBlocked(cluster, fmt.Sprintf("Cluster health gate blocked rollout at revision %q: %v", plan.UpdateRevision, err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	nextPod := plan.NextPodToDelete()
	if nextPod == nil {
		cluster.Status.LastOperation = succeededOperation("Rollout", fmt.Sprintf("Waiting for StatefulSet status to converge to revision %q", plan.UpdateRevision))
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             "WaitingForStatefulSetStatus",
			Message:            fmt.Sprintf("All pods have revision %q; waiting for StatefulSet status to report completion", plan.UpdateRevision),
			LastTransitionTime: metav1.Now(),
		})
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if err := r.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: nextPod.Namespace,
		Name:      nextPod.Name,
	}}); err != nil && !apierrors.IsNotFound(err) {
		r.markRolloutFailure(cluster, fmt.Sprintf("Delete pod %s failed: %v", nextPod.Name, err))
		return ctrl.Result{}, fmt.Errorf("delete rollout pod %s: %w", nextPod.Name, err)
	}

	cluster.Status.LastOperation = runningOperation("Rollout", fmt.Sprintf("Deleted pod %s to advance StatefulSet revision %q", nextPod.Name, plan.UpdateRevision))
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "PodDeleted",
		Message:            fmt.Sprintf("Deleted pod %s; waiting for replacement pod readiness and cluster convergence", nextPod.Name),
		LastTransitionTime: metav1.Now(),
	})

	return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func (r *NiFiClusterReconciler) finishSteadyState(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (ctrl.Result, error) {
	healthResult, err := r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             "WaitingForClusterHealth",
			Message:            "StatefulSet revisions match but the NiFi cluster health gate is not yet satisfied",
			LastTransitionTime: metav1.Now(),
		})
		r.markHealthGateBlocked(cluster, fmt.Sprintf("Steady-state health gate is still waiting: %v", err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	cluster.Status.ObservedStatefulSetRevision = target.Status.UpdateRevision
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutHealthy",
		Message:            "Target StatefulSet and NiFi cluster health are converged",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Idle",
		Message:            "No rollout is currently in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AsExpected",
		Message:            "No degradation detected",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = succeededOperation("Rollout", fmt.Sprintf("Revision %q is fully rolled out and healthy", target.Status.UpdateRevision))

	return ctrl.Result{}, nil
}

func (r *NiFiClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.HealthChecker == nil {
		r.HealthChecker = &LiveClusterHealthChecker{
			KubeClient: mgr.GetClient(),
			NiFiClient: nifi.NewHTTPClient(),
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NiFiCluster{}).
		Watches(&appsv1.StatefulSet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForStatefulSet)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.requestsForPod)).
		Complete(r)
}

func (r *NiFiClusterReconciler) requestsForStatefulSet(ctx context.Context, obj client.Object) []reconcile.Request {
	statefulSet, ok := obj.(*appsv1.StatefulSet)
	if !ok {
		return nil
	}
	return r.requestsForTarget(ctx, statefulSet.Namespace, statefulSet.Name)
}

func (r *NiFiClusterReconciler) requestsForPod(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind != "StatefulSet" {
			continue
		}
		return r.requestsForTarget(ctx, pod.Namespace, ownerRef.Name)
	}
	return nil
}

func (r *NiFiClusterReconciler) requestsForTarget(ctx context.Context, namespace, targetName string) []reconcile.Request {
	clusterList := &platformv1alpha1.NiFiClusterList{}
	if err := r.List(ctx, clusterList, client.InNamespace(namespace)); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(clusterList.Items))
	for _, cluster := range clusterList.Items {
		if cluster.Spec.TargetRef.Name != targetName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: cluster.Namespace,
				Name:      cluster.Name,
			},
		})
	}
	return requests
}

func (r *NiFiClusterReconciler) markSuspended(cluster *platformv1alpha1.NiFiCluster) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Availability is not evaluated while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            "No orchestration is in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "Suspended",
		Message:            "No active failure while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionUnknown,
		Reason:             "Suspended",
		Message:            "Hibernation state is not evaluated while reconciliation is suspended",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "StatusSync",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: "Reconciliation is suspended",
	}
}

func (r *NiFiClusterReconciler) markTargetMissing(cluster *platformv1alpha1.NiFiCluster, targetName string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionFalse,
		Reason:             "TargetNotFound",
		Message:            fmt.Sprintf("StatefulSet %q was not found", targetName),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetNotResolved",
		Message:            "Availability is unknown until the target StatefulSet exists",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "Idle",
		Message:            "No orchestration is in progress",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetNotFound",
		Message:            "Status cannot converge until the target StatefulSet exists",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionUnknown,
		Reason:             "TargetNotResolved",
		Message:            "Hibernation state is unknown until the target StatefulSet exists",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = failedOperation("StatusSync", fmt.Sprintf("Waiting for target StatefulSet %q", targetName))
}

func (r *NiFiClusterReconciler) markUnmanagedTarget(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) {
	cluster.Status.ObservedStatefulSetRevision = target.Status.UpdateRevision
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "UnmanagedTarget",
		Message:            "The target StatefulSet is not in managed OnDelete mode; controller rollout coordination is inactive",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "UnmanagedTarget",
		Message:            "Managed rollout coordination is disabled for this target",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "UnmanagedTarget",
		Message:            "No controller-managed rollout is active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = succeededOperation("StatusSync", "Target StatefulSet is unmanaged; controller rollout coordination is idle")
}

func (r *NiFiClusterReconciler) markUnsupportedDesiredState(cluster *platformv1alpha1.NiFiCluster) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "DesiredStateDeferred",
		Message:            "Only desiredState=Running is implemented in this rollout slice",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "DesiredStateDeferred",
		Message:            "Hibernation sequencing is intentionally deferred",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "DesiredStateDeferred",
		Message:            "No rollout failure is active; the requested desired state is deferred",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "NotImplemented",
		Message:            "Hibernation is not implemented yet",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "StatusSync",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: "DesiredState=Hibernated is deferred to a later slice",
	}
}

func (r *NiFiClusterReconciler) markHealthGateBlocked(cluster *platformv1alpha1.NiFiCluster, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "WaitingForClusterHealth",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "HealthGateBlocking",
		Message:            "Rollout is waiting for the documented per-pod health gate",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "HealthGateBlocking",
		Message:            "Cluster is not yet healthy enough to advance the rollout",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("Rollout", message)
}

func (r *NiFiClusterReconciler) markRolloutFailure(cluster *platformv1alpha1.NiFiCluster, message string) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "RolloutFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = failedOperation("Rollout", message)
}

func (r *NiFiClusterReconciler) applyClusterHealth(cluster *platformv1alpha1.NiFiCluster, result ClusterHealthResult) {
	cluster.Status.ClusterNodes.Connected = result.ConvergedPods
	cluster.Status.ClusterNodes.Disconnected = maxInt32(result.ExpectedReplicas-result.ConvergedPods, 0)
	cluster.Status.ClusterNodes.Offloaded = 0
}

func (r *NiFiClusterReconciler) syncReplicaStatus(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, pods []corev1.Pod) {
	cluster.Status.Replicas = platformv1alpha1.ReplicaStatus{
		Desired: derefInt32(target.Spec.Replicas),
		Ready:   target.Status.ReadyReplicas,
		Updated: target.Status.UpdatedReplicas,
	}

	if cluster.Status.ObservedStatefulSetRevision == "" && target.Status.CurrentRevision == target.Status.UpdateRevision {
		cluster.Status.ObservedStatefulSetRevision = target.Status.UpdateRevision
	}

	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.ClusterNodes.Offloaded = 0
	cluster.Status.Replicas.Ready = readyPodCount(pods)
}

func (r *NiFiClusterReconciler) listTargetPods(ctx context.Context, target *appsv1.StatefulSet) ([]corev1.Pod, error) {
	if target.Spec.Selector == nil {
		return nil, fmt.Errorf("target StatefulSet %q does not define a selector", target.Name)
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(target.Namespace),
		client.MatchingLabels(target.Spec.Selector.MatchLabels),
	); err != nil {
		return nil, fmt.Errorf("list pods for StatefulSet %q: %w", target.Name, err)
	}

	return podList.Items, nil
}

func (r *NiFiClusterReconciler) patchStatus(ctx context.Context, original, updated *platformv1alpha1.NiFiCluster, result ctrl.Result) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, updated.Status) {
		return result, nil
	}

	if err := r.Status().Patch(ctx, updated, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch NiFiCluster status: %w", err)
	}
	return result, nil
}

func runningOperation(operationType, message string) platformv1alpha1.LastOperation {
	now := metav1.NewTime(time.Now().UTC())
	return platformv1alpha1.LastOperation{
		Type:      operationType,
		Phase:     platformv1alpha1.OperationPhaseRunning,
		StartedAt: &now,
		Message:   message,
	}
}

func succeededOperation(operationType, message string) platformv1alpha1.LastOperation {
	now := metav1.NewTime(time.Now().UTC())
	return platformv1alpha1.LastOperation{
		Type:        operationType,
		Phase:       platformv1alpha1.OperationPhaseSucceeded,
		StartedAt:   &now,
		CompletedAt: &now,
		Message:     message,
	}
}

func failedOperation(operationType, message string) platformv1alpha1.LastOperation {
	now := metav1.NewTime(time.Now().UTC())
	return platformv1alpha1.LastOperation{
		Type:        operationType,
		Phase:       platformv1alpha1.OperationPhaseFailed,
		StartedAt:   &now,
		CompletedAt: &now,
		Message:     message,
	}
}

func podReadyTimeout(cluster *platformv1alpha1.NiFiCluster) time.Duration {
	if cluster.Spec.Rollout.PodReadyTimeout.Duration > 0 {
		return cluster.Spec.Rollout.PodReadyTimeout.Duration
	}
	return defaultPodReadyTimeout
}

func clusterHealthTimeout(cluster *platformv1alpha1.NiFiCluster) time.Duration {
	if cluster.Spec.Rollout.ClusterHealthTimeout.Duration > 0 {
		return cluster.Spec.Rollout.ClusterHealthTimeout.Duration
	}
	return defaultClusterHealthTimeout
}

func readyPodCount(pods []corev1.Pod) int32 {
	count := int32(0)
	for i := range pods {
		if isPodReady(&pods[i]) {
			count++
		}
	}
	return count
}

func maxInt32(value, floor int32) int32 {
	if value < floor {
		return floor
	}
	return value
}

func derefInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}
