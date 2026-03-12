package controller

import (
	"context"
	"fmt"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
)

const (
	autoscalingReasonDisabled           = "Disabled"
	autoscalingReasonTargetNotResolved  = "TargetNotResolved"
	autoscalingReasonUnmanagedTarget    = "UnmanagedTarget"
	autoscalingReasonHibernated         = "Hibernated"
	autoscalingReasonProgressing        = "Progressing"
	autoscalingReasonDegraded           = "Degraded"
	autoscalingReasonUnavailable        = "Unavailable"
	autoscalingReasonBelowMinReplicas   = "BelowMinReplicas"
	autoscalingReasonAboveMaxReplicas   = "AboveMaxReplicas"
	autoscalingReasonQueuePressure      = "QueuePressureDetected"
	autoscalingReasonCPUSaturation      = "CPUSaturationDetected"
	autoscalingReasonMaxReplicasReached = "MaxReplicasReached"
	autoscalingReasonNoActionableInput  = "NoActionableSignals"
)

func (r *NiFiClusterReconciler) syncAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) {
	desired, samples := r.buildAutoscalingStatus(ctx, cluster)
	if autoscalingStatusEqual(cluster.Status.Autoscaling, desired) {
		if cluster.Status.Autoscaling.LastEvaluationTime != nil {
			desired.LastEvaluationTime = cluster.Status.Autoscaling.LastEvaluationTime.DeepCopy()
		}
	} else {
		now := metav1.NewTime(time.Now().UTC())
		desired.LastEvaluationTime = &now
	}
	cluster.Status.Autoscaling = desired
	recordAutoscalingSignalSamples(cluster, samples)
}

func (r *NiFiClusterReconciler) buildAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	blocked, blockedReason := blockedAutoscalingStatus(cluster)
	if blocked {
		return platformv1alpha1.AutoscalingStatus{Reason: blockedReason}, autoscalingSignalCollection{}
	}

	policy := cluster.Spec.Autoscaling
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonDisabled}, autoscalingSignalCollection{}
	}

	target := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}, target); err != nil {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonTargetNotResolved}, autoscalingSignalCollection{}
	}

	signals := autoscalingSignals(policy)
	collection := autoscalingSignalCollection{
		SignalStatuses: make([]platformv1alpha1.AutoscalingSignalStatus, 0, len(signals)),
	}
	if r.AutoscalingCollector != nil {
		collection = r.AutoscalingCollector.Collect(ctx, cluster, target, signals)
	}
	if len(collection.SignalStatuses) == 0 {
		collection.SignalStatuses = unavailableAutoscalingSignals(signals, "autoscaling signal collector is not configured")
	}

	return buildAutoscalingSteadyStateStatus(cluster, policy, collection), collection
}

func blockedAutoscalingStatus(cluster *platformv1alpha1.NiFiCluster) (bool, string) {
	policy := cluster.Spec.Autoscaling
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		return true, autoscalingReasonDisabled
	}

	if !conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionTargetResolved)) {
		return true, autoscalingReasonTargetNotResolved
	}

	availableCondition := cluster.GetCondition(platformv1alpha1.ConditionAvailable)
	if availableCondition != nil && availableCondition.Reason == "UnmanagedTarget" {
		return true, autoscalingReasonUnmanagedTarget
	}

	if cluster.Spec.DesiredState == platformv1alpha1.DesiredStateHibernated || conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionHibernated)) {
		return true, autoscalingReasonHibernated
	}

	if conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionProgressing)) {
		return true, autoscalingReasonProgressing
	}

	if conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionDegraded)) {
		return true, autoscalingReasonDegraded
	}

	if !conditionIsTrue(cluster.GetCondition(platformv1alpha1.ConditionAvailable)) {
		return true, autoscalingReasonUnavailable
	}

	return false, ""
}

func buildAutoscalingSteadyStateStatus(cluster *platformv1alpha1.NiFiCluster, policy platformv1alpha1.AutoscalingPolicy, collection autoscalingSignalCollection) platformv1alpha1.AutoscalingStatus {
	currentReplicas := cluster.Status.Replicas.Desired
	minReplicas := autoscalingMinReplicas(policy, currentReplicas)
	maxReplicas := autoscalingMaxReplicas(policy, minReplicas, currentReplicas)

	recommended := currentReplicas
	reason := autoscalingReasonNoActionableInput

	switch {
	case currentReplicas < minReplicas:
		recommended = minReplicas
		reason = autoscalingReasonBelowMinReplicas
	case currentReplicas > maxReplicas:
		recommended = maxReplicas
		reason = autoscalingReasonAboveMaxReplicas
	case collection.QueuePressure.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonQueuePressure
	case collection.CPU.Actionable:
		recommended = currentReplicas + 1
		reason = autoscalingReasonCPUSaturation
	}

	if recommended > maxReplicas {
		if reason == autoscalingReasonQueuePressure || reason == autoscalingReasonCPUSaturation {
			recommended = maxReplicas
			reason = autoscalingReasonMaxReplicasReached
		} else {
			recommended = maxReplicas
			reason = autoscalingReasonAboveMaxReplicas
		}
	}

	return platformv1alpha1.AutoscalingStatus{
		RecommendedReplicas: ptrTo(recommended),
		Reason:              reason,
		Signals:             collection.SignalStatuses,
	}
}

func autoscalingMode(policy platformv1alpha1.AutoscalingPolicy) platformv1alpha1.AutoscalingMode {
	if policy.Mode == "" {
		return platformv1alpha1.AutoscalingModeDisabled
	}
	return policy.Mode
}

func autoscalingSignals(policy platformv1alpha1.AutoscalingPolicy) []platformv1alpha1.AutoscalingSignal {
	if len(policy.Signals) == 0 {
		return []platformv1alpha1.AutoscalingSignal{platformv1alpha1.AutoscalingSignalQueuePressure}
	}

	signals := make([]platformv1alpha1.AutoscalingSignal, 0, len(policy.Signals))
	for _, signal := range policy.Signals {
		if signal == "" || slices.Contains(signals, signal) {
			continue
		}
		signals = append(signals, signal)
	}
	if len(signals) == 0 {
		return []platformv1alpha1.AutoscalingSignal{platformv1alpha1.AutoscalingSignalQueuePressure}
	}
	return signals
}

func unavailableAutoscalingSignals(signals []platformv1alpha1.AutoscalingSignal, message string) []platformv1alpha1.AutoscalingSignalStatus {
	statuses := make([]platformv1alpha1.AutoscalingSignalStatus, 0, len(signals))
	for _, signal := range signals {
		statuses = append(statuses, platformv1alpha1.AutoscalingSignalStatus{
			Type:      signal,
			Available: false,
			Message:   message,
		})
	}
	return statuses
}

func autoscalingMinReplicas(policy platformv1alpha1.AutoscalingPolicy, current int32) int32 {
	if policy.MinReplicas > 0 {
		return policy.MinReplicas
	}
	if current > 0 {
		return current
	}
	return 1
}

func autoscalingMaxReplicas(policy platformv1alpha1.AutoscalingPolicy, minReplicas, current int32) int32 {
	maxReplicas := policy.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = current
	}
	if maxReplicas < minReplicas {
		return minReplicas
	}
	return maxReplicas
}

func autoscalingStatusEqual(left, right platformv1alpha1.AutoscalingStatus) bool {
	if left.Reason != right.Reason {
		return false
	}
	if !equalOptionalInt32(left.RecommendedReplicas, right.RecommendedReplicas) {
		return false
	}
	if len(left.Signals) != len(right.Signals) {
		return false
	}
	for i := range left.Signals {
		if left.Signals[i] != right.Signals[i] {
			return false
		}
	}
	return true
}

func autoscalingStatusMeaningEqual(left, right platformv1alpha1.AutoscalingStatus) bool {
	if left.Reason != right.Reason {
		return false
	}
	if !equalOptionalInt32(left.RecommendedReplicas, right.RecommendedReplicas) {
		return false
	}
	if len(left.Signals) != len(right.Signals) {
		return false
	}
	for i := range left.Signals {
		if left.Signals[i].Type != right.Signals[i].Type || left.Signals[i].Available != right.Signals[i].Available {
			return false
		}
	}
	return true
}

func equalOptionalInt32(left, right *int32) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

func autoscalingStatusMessage(status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return "Advisory autoscaling is disabled"
	case autoscalingReasonTargetNotResolved:
		return "Advisory autoscaling is waiting for the target StatefulSet to resolve"
	case autoscalingReasonUnmanagedTarget:
		return "Advisory autoscaling is blocked because the target StatefulSet is unmanaged"
	case autoscalingReasonHibernated:
		return "Advisory autoscaling is blocked while the cluster is hibernated or restoring"
	case autoscalingReasonProgressing:
		return "Advisory autoscaling is blocked while managed lifecycle work is in progress"
	case autoscalingReasonDegraded:
		return "Advisory autoscaling is blocked while the cluster is degraded"
	case autoscalingReasonUnavailable:
		return "Advisory autoscaling is blocked until the cluster is available"
	case autoscalingReasonBelowMinReplicas:
		return fmt.Sprintf("Advisory autoscaling recommends %d replicas because the current desired replica count is below the configured minimum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonAboveMaxReplicas:
		return fmt.Sprintf("Advisory autoscaling recommends %d replicas because the current desired replica count is above the configured maximum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonQueuePressure:
		return fmt.Sprintf("Advisory autoscaling recommends %d replicas because root process-group backlog is present while NiFi timer-driven threads are saturated. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonCPUSaturation:
		return fmt.Sprintf("Advisory autoscaling recommends %d replicas because NiFi system diagnostics report CPU saturation. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonMaxReplicasReached:
		return fmt.Sprintf("Advisory autoscaling is observing scale-up pressure, but the recommendation remains at %d replicas because the configured maximum has already been reached. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	default:
		summary := summarizeAutoscalingSignals(status.Signals)
		if summary == "" {
			return fmt.Sprintf("Advisory autoscaling recommends %d replicas; no actionable advisory signals are currently available", derefOptionalInt32(status.RecommendedReplicas))
		}
		return fmt.Sprintf("Advisory autoscaling recommends %d replicas; no actionable advisory signals are currently driving a scale-up. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summary)
	}
}

func autoscalingStatusOutcome(status platformv1alpha1.AutoscalingStatus) string {
	switch status.Reason {
	case autoscalingReasonDisabled:
		return "disabled"
	case autoscalingReasonBelowMinReplicas:
		return "increase"
	case autoscalingReasonAboveMaxReplicas:
		return "decrease"
	case autoscalingReasonQueuePressure, autoscalingReasonCPUSaturation:
		return "increase"
	case autoscalingReasonMaxReplicasReached, autoscalingReasonNoActionableInput:
		return "hold"
	default:
		return "blocked"
	}
}

func derefOptionalInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func conditionIsTrue(condition *metav1.Condition) bool {
	return condition != nil && condition.Status == metav1.ConditionTrue
}
