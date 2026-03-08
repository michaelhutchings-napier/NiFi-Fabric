package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
)

// NiFiClusterReconciler keeps the v0 scaffold intentionally small.
// It resolves the target StatefulSet and keeps status conditions fresh.
type NiFiClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *NiFiClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	cluster := &platformv1alpha1.NiFiCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	original := cluster.DeepCopy()
	cluster.InitializeConditions()
	cluster.Status.ObservedGeneration = cluster.Generation

	if cluster.Spec.Suspend {
		r.markSuspended(cluster)
		return r.patchStatus(ctx, original, cluster)
	}

	target := &appsv1.StatefulSet{}
	targetKey := client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}
	if err := r.Get(ctx, targetKey, target); err != nil {
		if apierrors.IsNotFound(err) {
			r.markTargetMissing(cluster, targetKey.Name)
			return r.patchStatus(ctx, original, cluster)
		}
		return ctrl.Result{}, fmt.Errorf("get target statefulset: %w", err)
	}

	r.syncFromStatefulSet(cluster, target)

	if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated && target.Status.ReadyReplicas == 0 {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionHibernated,
			Status:             metav1.ConditionTrue,
			Reason:             "ReplicasZero",
			Message:            "Target StatefulSet is scaled to zero replicas",
			LastTransitionTime: metav1.Now(),
		})
	} else {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionHibernated,
			Status:             metav1.ConditionFalse,
			Reason:             "RunningOrRestoring",
			Message:            "Cluster is not currently hibernated",
			LastTransitionTime: metav1.Now(),
		})
	}

	logger.V(1).Info("reconciled NiFiCluster scaffold status", "target", target.Name)
	return r.patchStatus(ctx, original, cluster)
}

func (r *NiFiClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NiFiCluster{}).
		Complete(r)
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
	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:      "StatusSync",
		Phase:     platformv1alpha1.OperationPhaseFailed,
		StartedAt: &now,
		Message:   fmt.Sprintf("Waiting for target StatefulSet %q", targetName),
	}
}

func (r *NiFiClusterReconciler) syncFromStatefulSet(cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) {
	cluster.Status.ObservedStatefulSetRevision = target.Status.UpdateRevision
	cluster.Status.Replicas = platformv1alpha1.ReplicaStatus{
		Desired: derefInt32(target.Spec.Replicas),
		Ready:   target.Status.ReadyReplicas,
		Updated: target.Status.UpdatedReplicas,
	}

	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionTargetResolved,
		Status:             metav1.ConditionTrue,
		Reason:             "TargetFound",
		Message:            "Target StatefulSet was resolved successfully",
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionFalse,
		Reason:             "ScaffoldOnly",
		Message:            "Advanced lifecycle orchestration is not implemented yet",
		LastTransitionTime: metav1.Now(),
	})

	if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateRunning && target.Status.ReadyReplicas == derefInt32(target.Spec.Replicas) {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionAvailable,
			Status:             metav1.ConditionTrue,
			Reason:             "ReadyReplicasSatisfied",
			Message:            "Target StatefulSet has the desired ready replicas",
			LastTransitionTime: metav1.Now(),
		})
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             "AsExpected",
			Message:            "No degradation detected in the scaffolded controller",
			LastTransitionTime: metav1.Now(),
		})
	} else {
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionAvailable,
			Status:             metav1.ConditionFalse,
			Reason:             "WaitingForReadyReplicas",
			Message:            "Target StatefulSet is not yet at the desired ready replica count",
			LastTransitionTime: metav1.Now(),
		})
		cluster.SetCondition(metav1.Condition{
			Type:               platformv1alpha1.ConditionDegraded,
			Status:             metav1.ConditionFalse,
			Reason:             "ReconcilingStatusOnly",
			Message:            "The scaffold is observing status only and not orchestrating lifecycle actions yet",
			LastTransitionTime: metav1.Now(),
		})
	}

	now := metav1.NewTime(time.Now().UTC())
	cluster.Status.LastOperation = platformv1alpha1.LastOperation{
		Type:      "StatusSync",
		Phase:     platformv1alpha1.OperationPhaseSucceeded,
		StartedAt: &now,
		Message:   "Target StatefulSet resolved and status synced",
	}
}

func (r *NiFiClusterReconciler) patchStatus(ctx context.Context, original, updated *platformv1alpha1.NiFiCluster) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original.Status, updated.Status) {
		return ctrl.Result{}, nil
	}

	if err := r.Status().Patch(ctx, updated, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patch NiFiCluster status: %w", err)
	}
	return ctrl.Result{}, nil
}

func derefInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}
