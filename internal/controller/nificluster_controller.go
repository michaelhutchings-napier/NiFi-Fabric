package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
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
	APIReader     client.Reader
	HealthChecker ClusterHealthChecker
	NodeManager   NodeManager
	Recorder      record.EventRecorder
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
	if r.NodeManager == nil {
		r.NodeManager = &LiveNodeManager{
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

	switch cluster.Spec.DesiredState {
	case platformv1alpha1.DesiredStateHibernated:
		return r.reconcileHibernation(ctx, cluster, target, pods)
	case platformv1alpha1.DesiredStateRunning:
		if restoreInProgress(cluster, target) {
			return r.reconcileRestore(ctx, cluster, target)
		}
	default:
		r.markUnsupportedDesiredState(cluster)
		return ctrl.Result{}, nil
	}

	drift, err := r.computeWatchedResourceDrift(ctx, cluster, target)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("compute watched-resource drift: %w", err)
	}

	r.syncObservedHashes(cluster, drift)
	drift, tlsResult, handledEarly, err := r.handleTLSPolicy(ctx, cluster, target, drift)
	if err != nil {
		return ctrl.Result{}, err
	}
	if handledEarly {
		return tlsResult, nil
	}

	r.startRevisionRolloutIfNeeded(cluster, target)
	r.startConfigRolloutIfNeeded(cluster, drift)

	plan := BuildRolloutPlan(target, pods, cluster.Status.Rollout)
	if !plan.HasDrift() {
		return r.finishSteadyState(ctx, cluster, target, drift)
	}

	progressReason, progressMessage := rolloutConditionDetails(plan, drift)
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             progressReason,
		Message:            progressMessage,
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutInProgress",
		Message:            availableRolloutMessage(plan),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "RolloutPending",
		Message:            degradedMessageForDrift(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("Rollout", rolloutOperationMessage(plan, drift))

	if currentNodeOpPod, ok := findNodeOperationPod(pods, cluster.Status.NodeOperation, platformv1alpha1.NodeOperationPurposeRestart); ok {
		if currentNodeOpPod.DeletionTimestamp != nil {
			cluster.SetCondition(metav1.Condition{
				Type:               platformv1alpha1.ConditionProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             "WaitingForReplacementPod",
				Message:            fmt.Sprintf("Pod %s is terminating; waiting for replacement pod readiness before continuing rollout", currentNodeOpPod.Name),
				LastTransitionTime: metav1.Now(),
			})
			cluster.Status.LastOperation = runningOperation("Rollout", fmt.Sprintf("Pod %s is terminating; waiting for replacement pod readiness before continuing rollout", currentNodeOpPod.Name))
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}

		prepared, result, err := r.preparePodForRestart(ctx, cluster, target, pods, currentNodeOpPod)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !prepared {
			return result, nil
		}

		if err := r.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: currentNodeOpPod.Namespace,
			Name:      currentNodeOpPod.Name,
		}}, client.Preconditions{
			UID: ptrTo(currentNodeOpPod.UID),
		}); err != nil && !apierrors.IsNotFound(err) {
			if apierrors.IsConflict(err) {
				cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
				cluster.SetCondition(metav1.Condition{
					Type:               platformv1alpha1.ConditionProgressing,
					Status:             metav1.ConditionTrue,
					Reason:             "RolloutStateRefreshPending",
					Message:            fmt.Sprintf("Pod %s was already recreated while rollout state was catching up; refreshing rollout progress", currentNodeOpPod.Name),
					LastTransitionTime: metav1.Now(),
				})
				return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
			}
			r.markRolloutFailure(cluster, fmt.Sprintf("Delete pod %s failed: %v", currentNodeOpPod.Name, err))
			return ctrl.Result{}, fmt.Errorf("delete rollout pod %s: %w", currentNodeOpPod.Name, err)
		}

		markRolloutPodCompleted(cluster, currentNodeOpPod.Name)
		cluster.Status.LastOperation = runningOperation("Rollout", deletedOperationMessage(currentNodeOpPod.Name, plan))
		cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             "PodDeleted",
			Message:            podDeletedMessage(currentNodeOpPod.Name, plan),
			LastTransitionTime: metav1.Now(),
		})

		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	if err := r.HealthChecker.WaitForPodsReady(ctx, target, podReadyTimeout(cluster)); err != nil {
		r.markHealthGateBlocked(cluster, fmt.Sprintf("Waiting for target pods to become Ready before deleting the next pod: %v", err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	healthResult, err := r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		r.markHealthGateBlocked(cluster, clusterHealthBlockedMessage(plan, err))
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}
	if podsPendingTermination(pods) {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionProgressing,
			Status:             metav1.ConditionTrue,
			Reason:             "WaitingForReplacementPod",
			Message:            "A rollout pod is still terminating; waiting for the replacement pod to settle before continuing",
			LastTransitionTime: metav1.Now(),
		})
		cluster.Status.LastOperation = runningOperation("Rollout", "A rollout pod is still terminating; waiting for the replacement pod to settle before continuing")
		return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
	}

	nextPod := plan.NextPodToDelete()
	if nextPod == nil && rolloutManagedByPodState(plan) {
		return r.finishSteadyState(ctx, cluster, target, drift)
	}
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

	prepared, result, err := r.preparePodForRestart(ctx, cluster, target, pods, *nextPod)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !prepared {
		return result, nil
	}

	if err := r.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: nextPod.Namespace,
		Name:      nextPod.Name,
	}}, client.Preconditions{
		UID: ptrTo(nextPod.UID),
	}); err != nil && !apierrors.IsNotFound(err) {
		if apierrors.IsConflict(err) {
			cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
			cluster.SetCondition(metav1.Condition{
				Type:               platformv1alpha1.ConditionProgressing,
				Status:             metav1.ConditionTrue,
				Reason:             "RolloutStateRefreshPending",
				Message:            fmt.Sprintf("Pod %s was already recreated while rollout state was catching up; refreshing rollout progress", nextPod.Name),
				LastTransitionTime: metav1.Now(),
			})
			return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
		}
		r.markRolloutFailure(cluster, fmt.Sprintf("Delete pod %s failed: %v", nextPod.Name, err))
		return ctrl.Result{}, fmt.Errorf("delete rollout pod %s: %w", nextPod.Name, err)
	}

	markRolloutPodCompleted(cluster, nextPod.Name)
	cluster.Status.LastOperation = runningOperation("Rollout", deletedOperationMessage(nextPod.Name, plan))
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "PodDeleted",
		Message:            podDeletedMessage(nextPod.Name, plan),
		LastTransitionTime: metav1.Now(),
	})

	return ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

func (r *NiFiClusterReconciler) finishSteadyState(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, drift WatchedResourceDrift) (ctrl.Result, error) {
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

	cluster.Status.ObservedStatefulSetRevision = steadyStateRevision(cluster, target)
	r.captureSteadyStateHashes(cluster, drift)
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
		Reason:             availableReasonForSteadyState(drift),
		Message:            availableMessageForSteadyState(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             progressingReasonForSteadyState(drift),
		Message:            progressingMessageForSteadyState(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             degradedReasonForSteadyState(drift),
		Message:            degradedMessageForSteadyState(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "Running",
		Message:            "Hibernation is not active",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = succeededOperation(lastOperationTypeForSteadyState(cluster), lastOperationMessageForSteadyState(cluster, target, drift))
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{}
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
	clearTLSObservation(cluster)

	return ctrl.Result{}, nil
}

func (r *NiFiClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.HealthChecker == nil {
		r.HealthChecker = &LiveClusterHealthChecker{
			KubeClient: mgr.GetClient(),
			NiFiClient: nifi.NewHTTPClient(),
		}
	}
	if r.NodeManager == nil {
		r.NodeManager = &LiveNodeManager{
			KubeClient: mgr.GetClient(),
			NiFiClient: nifi.NewHTTPClient(),
		}
	}
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("nificluster-controller")
	}
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NiFiCluster{}).
		Watches(&appsv1.StatefulSet{}, handler.EnqueueRequestsFromMapFunc(r.requestsForStatefulSet)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.requestsForPod)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.requestsForConfigMap)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.requestsForSecret)).
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

func (r *NiFiClusterReconciler) requestsForConfigMap(ctx context.Context, obj client.Object) []reconcile.Request {
	configMap, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	return r.requestsForWatchedResource(ctx, configMap.Namespace, configMap.Name, func(cluster platformv1alpha1.NiFiCluster) bool {
		for _, ref := range cluster.Spec.RestartTriggers.ConfigMaps {
			if ref.Name == configMap.Name {
				return true
			}
		}
		return false
	})
}

func (r *NiFiClusterReconciler) requestsForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	return r.requestsForWatchedResource(ctx, secret.Namespace, secret.Name, func(cluster platformv1alpha1.NiFiCluster) bool {
		for _, ref := range cluster.Spec.RestartTriggers.Secrets {
			if ref.Name == secret.Name {
				return true
			}
		}
		return false
	})
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
		requests = append(requests, reconcileRequestForCluster(cluster))
	}
	return requests
}

func (r *NiFiClusterReconciler) requestsForWatchedResource(ctx context.Context, namespace, resourceName string, matches func(platformv1alpha1.NiFiCluster) bool) []reconcile.Request {
	clusterList := &platformv1alpha1.NiFiClusterList{}
	if err := r.List(ctx, clusterList, client.InNamespace(namespace)); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(clusterList.Items))
	for _, cluster := range clusterList.Items {
		if !matches(cluster) {
			continue
		}
		requests = append(requests, reconcileRequestForCluster(cluster))
	}
	return requests
}

func reconcileRequestForCluster(cluster platformv1alpha1.NiFiCluster) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
	}
}

func (r *NiFiClusterReconciler) markSuspended(cluster *platformv1alpha1.NiFiCluster) {
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
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
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
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
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
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
	cluster.Status.NodeOperation = platformv1alpha1.NodeOperationStatus{}
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
		Reason:             "DesiredStateUnsupported",
		Message:            "Only desiredState=Running and desiredState=Hibernated are supported",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "DesiredStateUnsupported",
		Message:            "No orchestration is in progress for the requested desired state",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "DesiredStateUnsupported",
		Message:            "No rollout failure is active; the requested desired state is unsupported",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionHibernated,
		Status:             metav1.ConditionFalse,
		Reason:             "UnknownDesiredState",
		Message:            "Hibernation state is unknown for the requested desired state",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:    "StatusSync",
		Phase:   platformv1alpha1.OperationPhasePending,
		Message: "Requested desired state is unsupported by this controller",
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
	desiredReplicas := derefInt32(target.Spec.Replicas)
	cluster.Status.Replicas = platformv1alpha1.ReplicaStatus{
		Desired: desiredReplicas,
		Ready:   target.Status.ReadyReplicas,
		Updated: target.Status.UpdatedReplicas,
	}
	if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateRunning && desiredReplicas > cluster.Status.Hibernation.BaselineReplicas {
		cluster.Status.Hibernation.BaselineReplicas = desiredReplicas
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
	cluster.Status.ClusterNodes.Offloaded = 0
	cluster.Status.Replicas.Ready = readyPodCount(pods)
}

func rolloutConditionDetails(plan RolloutPlan, drift WatchedResourceDrift) (string, string) {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return "ConfigDriftDetected", configDriftMessage(drift)
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return "TLSRolloutInProgress", tlsRolloutMessage(drift)
	}
	return "RevisionDriftDetected", rolloutMessage(plan)
}

func rolloutOperationMessage(plan RolloutPlan, drift WatchedResourceDrift) string {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return fmt.Sprintf("Reconciling config drift toward hash %q from refs %s", drift.TargetConfigHash, joinOrNone(drift.ConfigRefs))
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return fmt.Sprintf("Reconciling TLS drift toward certificate hash %q from refs %s", clusterTargetCertificateHash(plan, drift), joinOrNone(drift.CertificateRefs))
	}
	return fmt.Sprintf("Reconciling revision drift from %q to %q", plan.CurrentRevision, plan.UpdateRevision)
}

func podDeletedMessage(podName string, plan RolloutPlan) string {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return fmt.Sprintf("Deleted pod %s to apply watched config drift; waiting for replacement pod readiness and cluster convergence", podName)
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return fmt.Sprintf("Deleted pod %s to apply TLS rollout; waiting for replacement pod readiness and cluster convergence", podName)
	}
	return fmt.Sprintf("Deleted pod %s; waiting for replacement pod readiness and cluster convergence", podName)
}

func deletedOperationMessage(podName string, plan RolloutPlan) string {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return fmt.Sprintf("Deleted pod %s to apply watched config drift", podName)
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return fmt.Sprintf("Deleted pod %s to apply TLS rollout", podName)
	}
	return fmt.Sprintf("Deleted pod %s to advance StatefulSet revision %q", podName, plan.UpdateRevision)
}

func availableRolloutMessage(plan RolloutPlan) string {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return "Waiting for watched config rollout pods and NiFi cluster health to converge"
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return "Waiting for TLS rollout pods and NiFi cluster health to converge"
	}
	return "Waiting for StatefulSet pods and NiFi cluster health to converge to the update revision"
}

func clusterHealthBlockedMessage(plan RolloutPlan, err error) string {
	if plan.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return fmt.Sprintf("Cluster health gate blocked watched config rollout: %v", err)
	}
	if plan.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return fmt.Sprintf("Cluster health gate blocked TLS rollout: %v", err)
	}
	return fmt.Sprintf("Cluster health gate blocked rollout at revision %q: %v", plan.UpdateRevision, err)
}

func degradedMessageForDrift(drift WatchedResourceDrift) string {
	return "No rollout failure is currently active"
}

func availableReasonForSteadyState(drift WatchedResourceDrift) string {
	if drift.TLSResolvedWithoutRestart {
		return "TLSDriftResolvedWithoutRestart"
	}
	return "RolloutHealthy"
}

func availableMessageForSteadyState(drift WatchedResourceDrift) string {
	if drift.TLSResolvedWithoutRestart {
		return "Target StatefulSet and NiFi cluster health stayed healthy while TLS drift was resolved without restart"
	}
	return "Target StatefulSet and NiFi cluster health are converged"
}

func progressingReasonForSteadyState(drift WatchedResourceDrift) string {
	if drift.TLSResolvedWithoutRestart {
		return "TLSDriftResolvedWithoutRestart"
	}
	return "NoDrift"
}

func progressingMessageForSteadyState(drift WatchedResourceDrift) string {
	if drift.TLSResolvedWithoutRestart {
		return "No rollout is in progress; TLS drift was resolved during the autoreload observation window"
	}
	return "No rollout is currently in progress and no watched drift is active"
}

func degradedReasonForSteadyState(drift WatchedResourceDrift) string {
	return "AsExpected"
}

func degradedMessageForSteadyState(drift WatchedResourceDrift) string {
	return "No degradation detected"
}

func lastOperationTypeForSteadyState(cluster *platformv1alpha1.NiFiCluster) string {
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerStatefulSetRevision {
		return "Rollout"
	}
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerConfigDrift {
		return "Rollout"
	}
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return "Rollout"
	}
	return "StatusSync"
}

func lastOperationMessageForSteadyState(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, drift WatchedResourceDrift) string {
	if drift.TLSResolvedWithoutRestart {
		return fmt.Sprintf("TLS drift resolved without restart and revision %q is healthy", steadyStateRevision(cluster, target))
	}
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		return fmt.Sprintf("TLS rollout completed and revision %q is healthy", steadyStateRevision(cluster, target))
	}
	return fmt.Sprintf("Revision %q is fully rolled out and healthy", steadyStateRevision(cluster, target))
}

func configDriftMessage(drift WatchedResourceDrift) string {
	return fmt.Sprintf("Watched config drift detected for %s; target hash is %q", joinOrNone(drift.ConfigRefs), drift.TargetConfigHash)
}

func certificateDriftMessage(drift WatchedResourceDrift) string {
	return fmt.Sprintf("Watched certificate drift detected for %s", joinOrNone(drift.CertificateRefs))
}

func tlsRolloutMessage(drift WatchedResourceDrift) string {
	return fmt.Sprintf("TLS rollout is in progress for %s", joinOrNone(drift.CertificateRefs))
}

func clusterTargetCertificateHash(plan RolloutPlan, drift WatchedResourceDrift) string {
	if drift.CurrentCertificateHash != "" {
		return drift.CurrentCertificateHash
	}
	return plan.UpdateRevision
}

func steadyStateRevision(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) string {
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerStatefulSetRevision && cluster.Status.Rollout.TargetRevision != "" {
		return cluster.Status.Rollout.TargetRevision
	}
	if target.Status.UpdateRevision != "" {
		return target.Status.UpdateRevision
	}
	return target.Status.CurrentRevision
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "no refs"
	}
	return strings.Join(values, ", ")
}

func (r *NiFiClusterReconciler) syncObservedHashes(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	if cluster.Status.ObservedConfigHash == "" && !drift.ConfigDrift {
		cluster.Status.ObservedConfigHash = drift.CurrentConfigHash
	}
	if cluster.Status.ObservedCertificateHash == "" && !drift.CertificateDrift {
		cluster.Status.ObservedCertificateHash = drift.CurrentCertificateHash
	}
	if cluster.Status.ObservedTLSConfigurationHash == "" && !drift.TLSConfigurationDrift {
		cluster.Status.ObservedTLSConfigurationHash = drift.CurrentTLSConfigurationHash
	}
}

func (r *NiFiClusterReconciler) startConfigRolloutIfNeeded(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	if cluster.Status.Rollout.Trigger != "" {
		return
	}
	if !drift.ConfigDrift {
		return
	}
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerConfigDrift &&
		cluster.Status.Rollout.TargetConfigHash == drift.TargetConfigHash &&
		cluster.Status.Rollout.StartedAt != nil {
		return
	}

	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:          platformv1alpha1.RolloutTriggerConfigDrift,
		StartedAt:        &now,
		TargetConfigHash: drift.CurrentConfigHash,
	}
}

func (r *NiFiClusterReconciler) startRevisionRolloutIfNeeded(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) {
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerStatefulSetRevision {
		if cluster.Status.Rollout.TargetRevision == "" && target.Status.UpdateRevision != "" {
			cluster.Status.Rollout.TargetRevision = target.Status.UpdateRevision
		}
		return
	}
	if cluster.Status.Rollout.Trigger != "" {
		return
	}

	targetRevision := target.Status.UpdateRevision
	if targetRevision == "" {
		return
	}
	if targetRevision == cluster.Status.ObservedStatefulSetRevision {
		return
	}

	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:        platformv1alpha1.RolloutTriggerStatefulSetRevision,
		StartedAt:      &now,
		TargetRevision: targetRevision,
	}
}

func (r *NiFiClusterReconciler) startTLSRolloutIfNeeded(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift &&
		cluster.Status.Rollout.TargetCertificateHash == drift.CurrentCertificateHash &&
		cluster.Status.Rollout.TargetTLSConfigurationHash == drift.CurrentTLSConfigurationHash &&
		cluster.Status.Rollout.StartedAt != nil {
		return
	}

	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.Rollout = platformv1alpha1.RolloutStatus{
		Trigger:                    platformv1alpha1.RolloutTriggerTLSDrift,
		StartedAt:                  &now,
		TargetCertificateHash:      drift.CurrentCertificateHash,
		TargetTLSConfigurationHash: drift.CurrentTLSConfigurationHash,
	}
	clearTLSObservation(cluster)
}

func (r *NiFiClusterReconciler) captureSteadyStateHashes(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerConfigDrift && cluster.Status.Rollout.TargetConfigHash != "" {
		cluster.Status.ObservedConfigHash = cluster.Status.Rollout.TargetConfigHash
	} else if !drift.ConfigDrift {
		cluster.Status.ObservedConfigHash = drift.CurrentConfigHash
	}

	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		if cluster.Status.Rollout.TargetCertificateHash != "" {
			cluster.Status.ObservedCertificateHash = cluster.Status.Rollout.TargetCertificateHash
		}
		if cluster.Status.Rollout.TargetTLSConfigurationHash != "" {
			cluster.Status.ObservedTLSConfigurationHash = cluster.Status.Rollout.TargetTLSConfigurationHash
		}
		return
	}

	if !drift.CertificateDrift || drift.TLSResolvedWithoutRestart {
		cluster.Status.ObservedCertificateHash = drift.CurrentCertificateHash
		cluster.Status.ObservedTLSConfigurationHash = drift.CurrentTLSConfigurationHash
	}
}

func (r *NiFiClusterReconciler) handleTLSPolicy(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, drift WatchedResourceDrift) (WatchedResourceDrift, ctrl.Result, bool, error) {
	if !drift.HasCertificateInputs {
		clearTLSObservation(cluster)
		return drift, ctrl.Result{}, false, nil
	}

	if cluster.Status.Rollout.Trigger != "" && cluster.Status.Rollout.Trigger != platformv1alpha1.RolloutTriggerTLSDrift {
		return drift, ctrl.Result{}, false, nil
	}

	if cluster.Status.Rollout.Trigger == platformv1alpha1.RolloutTriggerTLSDrift {
		drift.CertificateDrift = true
		return drift, ctrl.Result{}, false, nil
	}

	if !drift.CertificateDrift {
		clearTLSObservation(cluster)
		return drift, ctrl.Result{}, false, nil
	}

	if drift.MaterialTLSChange {
		r.startTLSRolloutIfNeeded(cluster, drift)
		return drift, ctrl.Result{}, false, nil
	}

	switch tlsDiffPolicy(cluster) {
	case platformv1alpha1.TLSDiffPolicyAlwaysRestart:
		r.startTLSRolloutIfNeeded(cluster, drift)
		return drift, ctrl.Result{}, false, nil
	case platformv1alpha1.TLSDiffPolicyObserveOnly:
		return r.observeTLSDiff(ctx, cluster, target, drift, false)
	default:
		return r.observeTLSDiff(ctx, cluster, target, drift, true)
	}
}

func (r *NiFiClusterReconciler) observeTLSDiff(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet, drift WatchedResourceDrift, restartOnFailure bool) (WatchedResourceDrift, ctrl.Result, bool, error) {
	startTLSObservation(cluster, drift)

	healthResult, err := r.HealthChecker.CheckClusterHealth(ctx, cluster, target)
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		if restartOnFailure {
			r.startTLSRolloutIfNeeded(cluster, drift)
			return drift, ctrl.Result{}, false, nil
		}
		r.markTLSObservationDegraded(cluster, drift, err)
		return drift, ctrl.Result{RequeueAfter: rolloutPollRequeue}, true, nil
	}

	if !tlsObservationElapsed(cluster) {
		r.markTLSObserving(cluster, drift)
		return drift, ctrl.Result{RequeueAfter: rolloutPollRequeue}, true, nil
	}

	healthResult, err = r.HealthChecker.WaitForClusterHealthy(ctx, cluster, target, clusterHealthTimeout(cluster))
	r.applyClusterHealth(cluster, healthResult)
	if err != nil {
		if restartOnFailure {
			r.startTLSRolloutIfNeeded(cluster, drift)
			return drift, ctrl.Result{}, false, nil
		}
		r.markTLSObservationDegraded(cluster, drift, err)
		return drift, ctrl.Result{RequeueAfter: rolloutPollRequeue}, true, nil
	}

	clearTLSObservation(cluster)
	cluster.Status.ObservedCertificateHash = drift.CurrentCertificateHash
	cluster.Status.ObservedTLSConfigurationHash = drift.CurrentTLSConfigurationHash
	drift.CertificateDrift = false
	drift.TLSConfigurationDrift = false
	drift.MaterialTLSChange = false
	drift.TLSResolvedWithoutRestart = true
	cluster.Status.LastOperation = succeededOperation("TLSObservation", fmt.Sprintf("TLS drift resolved without restart for %s", joinOrNone(drift.CertificateRefs)))
	return drift, ctrl.Result{}, false, nil
}

func (r *NiFiClusterReconciler) markTLSObserving(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionTrue,
		Reason:             "TLSDriftDetected",
		Message:            certificateDriftMessage(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "TLSAutoreloadObserving",
		Message:            fmt.Sprintf("Observing TLS autoreload for %s before deciding whether a restart is required", joinOrNone(drift.CertificateRefs)),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "TLSAutoreloadObserving",
		Message:            "No degradation detected during the TLS autoreload observation window",
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("TLSObservation", fmt.Sprintf("Observing TLS drift for %s", joinOrNone(drift.CertificateRefs)))
}

func (r *NiFiClusterReconciler) markTLSObservationDegraded(cluster *platformv1alpha1.NiFiCluster, drift WatchedResourceDrift, healthErr error) {
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "TLSAutoreloadHealthDegraded",
		Message:            "Cluster health degraded while TLS drift remained unresolved",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "TLSDriftDetected",
		Message:            certificateDriftMessage(drift),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             "TLSAutoreloadHealthDegraded",
		Message:            fmt.Sprintf("Cluster health degraded during TLS observation: %v", healthErr),
		LastTransitionTime: metav1.Now(),
	})
	cluster.Status.LastOperation = runningOperation("TLSObservation", fmt.Sprintf("TLS drift remains unresolved because cluster health degraded: %v", healthErr))
}

func (r *NiFiClusterReconciler) listTargetPods(ctx context.Context, target *appsv1.StatefulSet) ([]corev1.Pod, error) {
	return listTargetPodsWithReader(ctx, r.Client, target)
}

func listTargetPodsWithReader(ctx context.Context, reader client.Reader, target *appsv1.StatefulSet) ([]corev1.Pod, error) {
	if target.Spec.Selector == nil {
		return nil, fmt.Errorf("target StatefulSet %q does not define a selector", target.Name)
	}

	podList := &corev1.PodList{}
	if err := reader.List(ctx, podList,
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

	statusToPersist := updated.Status.DeepCopy()
	key := client.ObjectKeyFromObject(updated)
	statusReader := r.APIReader
	if statusReader == nil {
		statusReader = r.Client
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &platformv1alpha1.NiFiCluster{}
		if err := statusReader.Get(ctx, key, latest); err != nil {
			return err
		}
		latest.Status = *statusToPersist
		return r.Status().Update(ctx, latest)
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("update NiFiCluster status: %w", err)
	}
	r.observeStatusTransition(original, updated)
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

func markRolloutPodCompleted(cluster *platformv1alpha1.NiFiCluster, podName string) {
	if podName == "" {
		return
	}
	for _, existing := range cluster.Status.Rollout.CompletedPods {
		if existing == podName {
			return
		}
	}
	cluster.Status.Rollout.CompletedPods = append(cluster.Status.Rollout.CompletedPods, podName)
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

func ptrTo[T any](value T) *T {
	return &value
}
