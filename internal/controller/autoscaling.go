package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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

	defaultAutoscalingScaleUpCooldown = 5 * time.Minute
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
	desired.LastScalingDecision = cluster.Status.Autoscaling.LastScalingDecision
	if shouldRefreshAutoscalingDecision(cluster, desired) {
		desired.LastScalingDecision = autoscalingNoScaleDecision(cluster, desired)
	}
	if cluster.Status.Autoscaling.LastScaleUpTime != nil {
		desired.LastScaleUpTime = cluster.Status.Autoscaling.LastScaleUpTime.DeepCopy()
	}
	cluster.Status.Autoscaling = desired
	recordAutoscalingSignalSamples(cluster, samples)
}

func shouldRefreshAutoscalingDecision(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) bool {
	if strings.HasPrefix(cluster.Status.Autoscaling.LastScalingDecision, "ScaleUp:") {
		return false
	}
	if condition := cluster.GetCondition(platformv1alpha1.ConditionProgressing); conditionIsTrue(condition) && condition.Reason == "AutoscalingScaleUp" {
		return false
	}

	policy := cluster.Spec.Autoscaling
	if autoscalingMode(policy) != platformv1alpha1.AutoscalingModeEnforced {
		return true
	}
	if !policy.ScaleUp.Enabled {
		return true
	}
	if status.RecommendedReplicas == nil {
		return true
	}
	return derefOptionalInt32(status.RecommendedReplicas) <= cluster.Status.Replicas.Desired
}

func autoscalingNoScaleDecision(cluster *platformv1alpha1.NiFiCluster, status platformv1alpha1.AutoscalingStatus) string {
	policy := cluster.Spec.Autoscaling
	switch autoscalingMode(policy) {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		return "NoScaleUp: autoscaling is not in enforced mode"
	}

	if !policy.ScaleUp.Enabled {
		return "NoScaleUp: scale-up is not enabled"
	}
	if status.RecommendedReplicas == nil {
		return fmt.Sprintf("NoScaleUp: recommendation is unavailable because %s", status.Reason)
	}

	if derefOptionalInt32(status.RecommendedReplicas) < cluster.Status.Replicas.Desired {
		return "NoScaleUp: automatic scale-down is disabled"
	}
	return "NoScaleUp: recommended replicas are already satisfied"
}

func (r *NiFiClusterReconciler) buildAutoscalingStatus(ctx context.Context, cluster *platformv1alpha1.NiFiCluster) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	target := &appsv1.StatefulSet{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Spec.TargetRef.Name}, target); err != nil {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonTargetNotResolved}, autoscalingSignalCollection{}
	}

	return r.buildAutoscalingStatusForTarget(ctx, cluster, target)
}

func (r *NiFiClusterReconciler) buildAutoscalingStatusForTarget(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (platformv1alpha1.AutoscalingStatus, autoscalingSignalCollection) {
	policy := cluster.Spec.Autoscaling
	blocked, blockedReason := blockedAutoscalingStatus(cluster)
	if blocked {
		return platformv1alpha1.AutoscalingStatus{Reason: blockedReason}, autoscalingSignalCollection{}
	}
	if autoscalingMode(policy) == platformv1alpha1.AutoscalingModeDisabled {
		return platformv1alpha1.AutoscalingStatus{Reason: autoscalingReasonDisabled}, autoscalingSignalCollection{}
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

func (r *NiFiClusterReconciler) maybeExecuteAutoscalingScaleUp(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) (bool, ctrl.Result, error) {
	status, _ := r.buildAutoscalingStatusForTarget(ctx, cluster, target)
	executionState := r.autoscalingExecutionState(ctx, cluster, target)

	policy := cluster.Spec.Autoscaling
	mode := autoscalingMode(policy)
	switch mode {
	case platformv1alpha1.AutoscalingModeDisabled, platformv1alpha1.AutoscalingModeAdvisory:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: autoscaling is not in enforced mode"
		return false, ctrl.Result{}, nil
	}

	if !policy.ScaleUp.Enabled {
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: scale-up is not enabled"
		return false, ctrl.Result{}, nil
	}
	if status.RecommendedReplicas == nil {
		cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleUp: recommendation is unavailable because %s", status.Reason)
		return false, ctrl.Result{}, nil
	}

	currentReplicas := executionState.currentReplicas
	recommendedReplicas := derefOptionalInt32(status.RecommendedReplicas)
	switch {
	case recommendedReplicas < currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: automatic scale-down is disabled"
		return false, ctrl.Result{}, nil
	case recommendedReplicas == currentReplicas:
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: recommended replicas are already satisfied"
		return false, ctrl.Result{}, nil
	}

	cooldown := autoscalingScaleUpCooldown(policy)
	if executionState.lastScaleUpTime != nil && cooldown > 0 {
		nextEligibleTime := executionState.lastScaleUpTime.Time.Add(cooldown)
		if time.Now().UTC().Before(nextEligibleTime) {
			cluster.Status.Autoscaling.LastScalingDecision = fmt.Sprintf("NoScaleUp: cooldown is active until %s", nextEligibleTime.UTC().Format(time.RFC3339))
			return false, ctrl.Result{}, nil
		}
	}

	desiredReplicas := currentReplicas + 1
	if desiredReplicas > recommendedReplicas {
		desiredReplicas = recommendedReplicas
	}
	maxReplicas := autoscalingMaxReplicas(policy, autoscalingMinReplicas(policy, currentReplicas), currentReplicas)
	if desiredReplicas > maxReplicas {
		desiredReplicas = maxReplicas
	}
	if desiredReplicas <= currentReplicas {
		cluster.Status.Autoscaling.LastScalingDecision = "NoScaleUp: bounded recommendation does not allow a larger target size"
		return false, ctrl.Result{}, nil
	}

	scaledTarget := target.DeepCopy()
	scaledTarget.Spec.Replicas = ptrTo(desiredReplicas)
	if err := r.Update(ctx, scaledTarget); err != nil {
		return false, ctrl.Result{}, fmt.Errorf("update StatefulSet replicas for autoscaling scale-up: %w", err)
	}

	now := metav1.NewTime(time.Now().UTC())
	decision := fmt.Sprintf("ScaleUp: increased target StatefulSet replicas from %d to %d", currentReplicas, desiredReplicas)
	cluster.Status.Autoscaling.LastScalingDecision = decision
	cluster.Status.Autoscaling.LastScaleUpTime = &now
	cluster.Status.LastOperation = runningOperation("AutoscalingScaleUp", fmt.Sprintf("%s because %s", decision, autoscalingStatusMessage(status)))
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionProgressing,
		Status:             metav1.ConditionTrue,
		Reason:             "AutoscalingScaleUp",
		Message:            fmt.Sprintf("Managed autoscaling is scaling the cluster from %d to %d replicas", currentReplicas, desiredReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionAvailable,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleUp",
		Message:            fmt.Sprintf("Waiting for the scaled cluster to become healthy at %d replicas", desiredReplicas),
		LastTransitionTime: metav1.Now(),
	})
	cluster.SetCondition(metav1.Condition{
		Type:               platformv1alpha1.ConditionDegraded,
		Status:             metav1.ConditionFalse,
		Reason:             "AutoscalingScaleUp",
		Message:            "Autoscaling scale-up is in progress",
		LastTransitionTime: metav1.Now(),
	})
	recordAutoscalingScaleAction("scaled_up")
	if r.Recorder != nil {
		r.Recorder.Event(cluster, "Normal", "AutoscalingScaleUp", fmt.Sprintf("Managed autoscaling increased replicas from %d to %d", currentReplicas, desiredReplicas))
	}

	return true, ctrl.Result{RequeueAfter: rolloutPollRequeue}, nil
}

type autoscalingExecutionState struct {
	currentReplicas int32
	lastScaleUpTime *metav1.Time
}

func (r *NiFiClusterReconciler) autoscalingExecutionState(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, target *appsv1.StatefulSet) autoscalingExecutionState {
	state := autoscalingExecutionState{
		currentReplicas: cluster.Status.Replicas.Desired,
	}
	if cluster.Status.Autoscaling.LastScaleUpTime != nil {
		state.lastScaleUpTime = cluster.Status.Autoscaling.LastScaleUpTime.DeepCopy()
	}
	if target != nil && target.Spec.Replicas != nil && *target.Spec.Replicas > state.currentReplicas {
		state.currentReplicas = *target.Spec.Replicas
	}

	if r.APIReader == nil {
		return state
	}

	latestCluster := &platformv1alpha1.NiFiCluster{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(cluster), latestCluster); err != nil {
		return state
	}
	if latestCluster.Status.Replicas.Desired > state.currentReplicas {
		state.currentReplicas = latestCluster.Status.Replicas.Desired
	}
	if latestCluster.Status.Autoscaling.LastScaleUpTime != nil {
		if state.lastScaleUpTime == nil || latestCluster.Status.Autoscaling.LastScaleUpTime.After(state.lastScaleUpTime.Time) {
			state.lastScaleUpTime = latestCluster.Status.Autoscaling.LastScaleUpTime.DeepCopy()
		}
	}
	return state
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

func autoscalingScaleUpCooldown(policy platformv1alpha1.AutoscalingPolicy) time.Duration {
	if policy.ScaleUp.Cooldown.Duration > 0 {
		return policy.ScaleUp.Cooldown.Duration
	}
	return defaultAutoscalingScaleUpCooldown
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
		return "Autoscaling is disabled"
	case autoscalingReasonTargetNotResolved:
		return "Autoscaling is waiting for the target StatefulSet to resolve"
	case autoscalingReasonUnmanagedTarget:
		return "Autoscaling is blocked because the target StatefulSet is unmanaged"
	case autoscalingReasonHibernated:
		return "Autoscaling is blocked while the cluster is hibernated or restoring"
	case autoscalingReasonProgressing:
		return "Autoscaling is blocked while managed lifecycle work is in progress"
	case autoscalingReasonDegraded:
		return "Autoscaling is blocked while the cluster is degraded"
	case autoscalingReasonUnavailable:
		return "Autoscaling is blocked until the cluster is available"
	case autoscalingReasonBelowMinReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is below the configured minimum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonAboveMaxReplicas:
		return fmt.Sprintf("Autoscaling recommends %d replicas because the current desired replica count is above the configured maximum", derefOptionalInt32(status.RecommendedReplicas))
	case autoscalingReasonQueuePressure:
		return fmt.Sprintf("Autoscaling recommends %d replicas because root process-group backlog is present while NiFi timer-driven threads are saturated. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonCPUSaturation:
		return fmt.Sprintf("Autoscaling recommends %d replicas because NiFi system diagnostics report CPU saturation. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	case autoscalingReasonMaxReplicasReached:
		return fmt.Sprintf("Autoscaling is observing scale-up pressure, but the recommendation remains at %d replicas because the configured maximum has already been reached. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summarizeAutoscalingSignals(status.Signals))
	default:
		summary := summarizeAutoscalingSignals(status.Signals)
		if summary == "" {
			return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently available", derefOptionalInt32(status.RecommendedReplicas))
		}
		return fmt.Sprintf("Autoscaling recommends %d replicas; no actionable signals are currently driving a scale-up. Signals: %s", derefOptionalInt32(status.RecommendedReplicas), summary)
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
