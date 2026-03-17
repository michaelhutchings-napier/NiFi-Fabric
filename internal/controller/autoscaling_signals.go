package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/NiFi-Fabric/api/v1alpha1"
	"github.com/michaelhutchings-napier/NiFi-Fabric/internal/nifi"
)

type AutoscalingSignalCollector interface {
	Collect(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, signals []platformv1alpha1.AutoscalingSignal) autoscalingSignalCollection
}

type autoscalingSignalCollection struct {
	SignalStatuses []platformv1alpha1.AutoscalingSignalStatus
	QueuePressure  autoscalingQueuePressureSample
	CPU            autoscalingCPUSample
}

type autoscalingQueuePressureSample struct {
	Observed                 bool
	FlowFilesQueued          int64
	BytesQueued              int64
	BytesQueuedObserved      bool
	BacklogPresent           bool
	ActiveTimerDrivenThreads int32
	MaxTimerDrivenThreads    int32
	ThreadCountsObserved     bool
	PressureBuilding         bool
	Actionable               bool
	PendingConfirmation      bool
	LowPressure              bool
	Interpretation           string
}

type autoscalingCPUSample struct {
	Observed            bool
	LoadAverage         float64
	AvailableProcessors int32
	Actionable          bool
	PendingConfirmation bool
	Interpretation      string
}

type LiveAutoscalingSignalCollector struct {
	KubeClient client.Client
	NiFiClient nifi.Client
}

func (c *LiveAutoscalingSignalCollector) Collect(ctx context.Context, _ *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, signals []platformv1alpha1.AutoscalingSignal) autoscalingSignalCollection {
	collection := autoscalingSignalCollection{
		SignalStatuses: make([]platformv1alpha1.AutoscalingSignalStatus, 0, len(signals)),
	}

	if c.KubeClient == nil || c.NiFiClient == nil {
		for _, signal := range signals {
			collection.SignalStatuses = append(collection.SignalStatuses, platformv1alpha1.AutoscalingSignalStatus{
				Type:      signal,
				Available: false,
				Message:   "autoscaling signal collector is not configured",
			})
		}
		return collection
	}

	pod, auth, caCertPEM, err := c.resolveRequestContext(ctx, sts)
	if err != nil {
		for _, signal := range signals {
			collection.SignalStatuses = append(collection.SignalStatuses, platformv1alpha1.AutoscalingSignalStatus{
				Type:      signal,
				Available: false,
				Message:   fmt.Sprintf("autoscaling signal collection is unavailable: %s", truncateError(err)),
			})
		}
		return collection
	}

	request := nifi.APIRequest{
		BaseURL:   podBaseURL(sts, pod),
		Username:  auth.Username,
		Password:  auth.Password,
		CACertPEM: caCertPEM,
	}

	needsQueuePressure := signalEnabled(signals, platformv1alpha1.AutoscalingSignalQueuePressure)
	needsCPU := signalEnabled(signals, platformv1alpha1.AutoscalingSignalCPU)

	var (
		rootStatus        nifi.RootProcessGroupStatus
		rootStatusErr     error
		systemDiagnostics nifi.SystemDiagnostics
		systemDiagErr     error
	)

	if needsQueuePressure {
		rootStatus, rootStatusErr = c.NiFiClient.GetRootProcessGroupStatus(ctx, request)
	}
	if needsQueuePressure || needsCPU {
		systemDiagnostics, systemDiagErr = c.NiFiClient.GetSystemDiagnostics(ctx, request)
	}

	if needsQueuePressure && rootStatusErr == nil {
		collection.QueuePressure.Observed = true
		collection.QueuePressure.FlowFilesQueued = rootStatus.FlowFilesQueued
		collection.QueuePressure.BytesQueued = rootStatus.BytesQueued
		collection.QueuePressure.BytesQueuedObserved = rootStatus.BytesQueuedObserved
		collection.QueuePressure.BacklogPresent = rootStatus.FlowFilesQueued > 0 ||
			(rootStatus.BytesQueuedObserved && rootStatus.BytesQueued > 0)
		collection.QueuePressure.LowPressure = rootStatus.FlowFilesQueued == 0 &&
			(!rootStatus.BytesQueuedObserved || rootStatus.BytesQueued == 0)
	}
	if needsQueuePressure && systemDiagErr == nil {
		collection.QueuePressure.ActiveTimerDrivenThreads = systemDiagnostics.ActiveTimerDrivenThreads
		collection.QueuePressure.MaxTimerDrivenThreads = systemDiagnostics.MaxTimerDrivenThreads
		collection.QueuePressure.ThreadCountsObserved = systemDiagnostics.ThreadCountsObserved
		if collection.QueuePressure.Observed &&
			collection.QueuePressure.ThreadCountsObserved &&
			collection.QueuePressure.MaxTimerDrivenThreads > 0 &&
			collection.QueuePressure.BacklogPresent {
			if collection.QueuePressure.ActiveTimerDrivenThreads >= collection.QueuePressure.MaxTimerDrivenThreads {
				collection.QueuePressure.Actionable = true
			}
			if collection.QueuePressure.ActiveTimerDrivenThreads >= autoscalingScaleUpThreadThreshold(collection.QueuePressure.MaxTimerDrivenThreads) {
				collection.QueuePressure.PressureBuilding = true
			}
		}
	}
	if needsCPU && systemDiagErr == nil {
		collection.CPU.Observed = systemDiagnostics.CPULoadObserved && systemDiagnostics.AvailableProcessors > 0
		collection.CPU.LoadAverage = systemDiagnostics.CPULoadAverage
		collection.CPU.AvailableProcessors = systemDiagnostics.AvailableProcessors
		if collection.CPU.Observed && collection.CPU.LoadAverage >= float64(collection.CPU.AvailableProcessors) {
			collection.CPU.Actionable = true
		}
	}
	if collection.QueuePressure.LowPressure && collection.CPU.Actionable {
		collection.QueuePressure.LowPressure = false
	}
	for _, signal := range signals {
		switch signal {
		case platformv1alpha1.AutoscalingSignalCPU:
			collection.SignalStatuses = append(collection.SignalStatuses, buildCPUSignalStatus(collection.CPU, systemDiagErr))
		default:
			collection.SignalStatuses = append(collection.SignalStatuses, buildQueuePressureSignalStatus(collection.QueuePressure, rootStatusErr, systemDiagErr))
		}
	}

	return collection
}

func (c *LiveAutoscalingSignalCollector) resolveRequestContext(ctx context.Context, sts *appsv1.StatefulSet) (*corev1.Pod, authConfig, []byte, error) {
	pods, err := c.listTargetPods(ctx, sts)
	if err != nil {
		return nil, authConfig{}, nil, err
	}
	pod, err := selectAutoscalingPod(pods)
	if err != nil {
		return nil, authConfig{}, nil, err
	}
	auth, err := resolveAuthConfig(ctx, c.KubeClient, sts)
	if err != nil {
		return nil, authConfig{}, nil, err
	}
	caCertPEM, err := resolveCACert(ctx, c.KubeClient, sts)
	if err != nil {
		return nil, authConfig{}, nil, err
	}
	return pod, auth, caCertPEM, nil
}

func (c *LiveAutoscalingSignalCollector) listTargetPods(ctx context.Context, sts *appsv1.StatefulSet) ([]corev1.Pod, error) {
	if sts.Spec.Selector == nil {
		return nil, fmt.Errorf("target StatefulSet %q does not define a selector", sts.Name)
	}

	podList := &corev1.PodList{}
	if err := c.KubeClient.List(ctx, podList,
		client.InNamespace(sts.Namespace),
		client.MatchingLabels(sts.Spec.Selector.MatchLabels),
	); err != nil {
		return nil, fmt.Errorf("list target pods: %w", err)
	}
	return podList.Items, nil
}

func selectAutoscalingPod(pods []corev1.Pod) (*corev1.Pod, error) {
	if len(pods) == 0 {
		return nil, fmt.Errorf("no target pods were found for autoscaling signal collection")
	}

	sorted := append([]corev1.Pod(nil), pods...)
	sort.Slice(sorted, func(i, j int) bool {
		leftOrdinal, leftOK := podOrdinal(&sorted[i])
		rightOrdinal, rightOK := podOrdinal(&sorted[j])
		switch {
		case leftOK && rightOK:
			return leftOrdinal < rightOrdinal
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return sorted[i].Name < sorted[j].Name
		}
	})

	for i := range sorted {
		if sorted[i].DeletionTimestamp == nil && isPodReady(&sorted[i]) {
			return &sorted[i], nil
		}
	}
	for i := range sorted {
		if sorted[i].DeletionTimestamp == nil {
			return &sorted[i], nil
		}
	}
	return nil, fmt.Errorf("no non-terminating target pods are available for autoscaling signal collection")
}

func signalEnabled(signals []platformv1alpha1.AutoscalingSignal, target platformv1alpha1.AutoscalingSignal) bool {
	for _, signal := range signals {
		if signal == target {
			return true
		}
	}
	return false
}

func buildQueuePressureSignalStatus(sample autoscalingQueuePressureSample, queueErr, diagnosticsErr error) platformv1alpha1.AutoscalingSignalStatus {
	if queueErr != nil {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: false,
			Message:   fmt.Sprintf("queue-pressure sampling failed: %s", truncateError(queueErr)),
		}
	}
	if diagnosticsErr != nil {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: false,
			Message:   fmt.Sprintf("queue-pressure sampled queuedFlowFiles=%d but thread saturation is unavailable: %s", sample.FlowFilesQueued, truncateError(diagnosticsErr)),
		}
	}
	if !sample.Observed {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: false,
			Message:   "queue-pressure sampling did not return root process-group backlog values",
		}
	}
	if !sample.ThreadCountsObserved {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
			Available: false,
			Message:   fmt.Sprintf("queuedFlowFiles=%d queuedBytes=%s but timer-driven thread counts were not reported by NiFi", sample.FlowFilesQueued, formatObservedBytes(sample.BytesQueued, sample.BytesQueuedObserved)),
		}
	}

	message := fmt.Sprintf(
		"queuedFlowFiles=%d queuedBytes=%s activeTimerDrivenThreads=%d/%d",
		sample.FlowFilesQueued,
		formatObservedBytes(sample.BytesQueued, sample.BytesQueuedObserved),
		sample.ActiveTimerDrivenThreads,
		sample.MaxTimerDrivenThreads,
	)
	if sample.Actionable {
		message += " " + emptyIfUnset(sample.Interpretation, "backlog is actionable")
	} else if sample.PendingConfirmation {
		message += " " + emptyIfUnset(sample.Interpretation, "backlog pressure is building and needs one more corroborating evaluation before scale-up")
	} else if sample.LowPressure {
		threshold := autoscalingLowPressureActiveThreadThreshold(sample.MaxTimerDrivenThreads)
		if sample.ActiveTimerDrivenThreads > threshold {
			message += fmt.Sprintf(" backlog is zero but active timer-driven work is still above the low-pressure threshold %d", threshold)
		} else {
			message += " backlog is low"
		}
	} else if sample.BacklogPresent {
		message += " " + emptyIfUnset(sample.Interpretation, "backlog is present but executor saturation is below the scale-up threshold")
	}

	return platformv1alpha1.AutoscalingSignalStatus{
		Type:      platformv1alpha1.AutoscalingSignalQueuePressure,
		Available: true,
		Message:   message,
	}
}

func buildCPUSignalStatus(sample autoscalingCPUSample, diagnosticsErr error) platformv1alpha1.AutoscalingSignalStatus {
	if diagnosticsErr != nil {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalCPU,
			Available: false,
			Message:   fmt.Sprintf("CPU sampling failed: %s", truncateError(diagnosticsErr)),
		}
	}
	if !sample.Observed {
		return platformv1alpha1.AutoscalingSignalStatus{
			Type:      platformv1alpha1.AutoscalingSignalCPU,
			Available: false,
			Message:   "CPU sampling is not available from NiFi system diagnostics yet",
		}
	}

	message := fmt.Sprintf("loadAverage=%.2f availableProcessors=%d", sample.LoadAverage, sample.AvailableProcessors)
	if sample.Actionable {
		message += " " + emptyIfUnset(sample.Interpretation, "saturation is actionable")
	} else if sample.PendingConfirmation {
		message += " " + emptyIfUnset(sample.Interpretation, "saturation is high but needs one more corroborating evaluation or root-process-group backlog before scale-up")
	}
	return platformv1alpha1.AutoscalingSignalStatus{
		Type:      platformv1alpha1.AutoscalingSignalCPU,
		Available: true,
		Message:   message,
	}
}

func qualifyAutoscalingSignalCollection(previous platformv1alpha1.AutoscalingStatus, collection autoscalingSignalCollection) autoscalingSignalCollection {
	queueSeenBefore := autoscalingPreviousQueuePressureObserved(previous)
	cpuSeenBefore := autoscalingPreviousCPUSaturationObserved(previous)

	if collection.QueuePressure.BacklogPresent {
		switch {
		case collection.QueuePressure.Actionable && collection.CPU.Actionable:
			collection.QueuePressure.Interpretation = "backlog is actionable because simultaneous CPU saturation corroborates the queue backlog"
		case collection.QueuePressure.Actionable && queueSeenBefore:
			collection.QueuePressure.Interpretation = "backlog is actionable because queue pressure persisted across consecutive evaluations"
		case collection.QueuePressure.Actionable:
			collection.QueuePressure.Actionable = false
			collection.QueuePressure.PendingConfirmation = true
			collection.QueuePressure.Interpretation = "backlog pressure is building and needs one more corroborating evaluation before scale-up"
		case collection.QueuePressure.PressureBuilding && collection.CPU.Actionable:
			collection.QueuePressure.Actionable = true
			collection.QueuePressure.Interpretation = "backlog is actionable because simultaneous CPU saturation corroborates the queue backlog"
		case collection.QueuePressure.PressureBuilding && queueSeenBefore:
			collection.QueuePressure.Actionable = true
			collection.QueuePressure.Interpretation = "backlog is actionable because queue pressure persisted across consecutive evaluations"
		case collection.QueuePressure.PressureBuilding:
			collection.QueuePressure.PendingConfirmation = true
			collection.QueuePressure.Interpretation = "backlog pressure is building and needs one more corroborating evaluation before scale-up"
		default:
			collection.QueuePressure.Interpretation = "backlog is present but executor saturation is below the scale-up threshold"
		}
	}

	if collection.CPU.Actionable {
		switch {
		case collection.QueuePressure.BacklogPresent:
			collection.CPU.Interpretation = "saturation is actionable because root-process-group backlog corroborates the CPU pressure"
		case cpuSeenBefore:
			collection.CPU.Interpretation = "saturation is actionable because CPU pressure persisted across consecutive evaluations"
		default:
			collection.CPU.Actionable = false
			collection.CPU.PendingConfirmation = true
			collection.CPU.Interpretation = "saturation is high but needs one more corroborating evaluation or root-process-group backlog before scale-up"
		}
	}

	for i := range collection.SignalStatuses {
		if !collection.SignalStatuses[i].Available {
			continue
		}
		switch collection.SignalStatuses[i].Type {
		case platformv1alpha1.AutoscalingSignalCPU:
			collection.SignalStatuses[i] = buildCPUSignalStatus(collection.CPU, nil)
		case platformv1alpha1.AutoscalingSignalQueuePressure:
			collection.SignalStatuses[i] = buildQueuePressureSignalStatus(collection.QueuePressure, nil, nil)
		}
	}

	return collection
}

func autoscalingPreviousQueuePressureObserved(previous platformv1alpha1.AutoscalingStatus) bool {
	return autoscalingSignalMessageContains(previous.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "backlog is actionable") ||
		autoscalingSignalMessageContains(previous.Signals, platformv1alpha1.AutoscalingSignalQueuePressure, "backlog pressure is building")
}

func autoscalingPreviousCPUSaturationObserved(previous platformv1alpha1.AutoscalingStatus) bool {
	return autoscalingSignalMessageContains(previous.Signals, platformv1alpha1.AutoscalingSignalCPU, "saturation is actionable") ||
		autoscalingSignalMessageContains(previous.Signals, platformv1alpha1.AutoscalingSignalCPU, "saturation is high but needs one more corroborating evaluation")
}

func autoscalingSignalMessageContains(signals []platformv1alpha1.AutoscalingSignalStatus, signalType platformv1alpha1.AutoscalingSignal, fragment string) bool {
	for _, signal := range signals {
		if signal.Type != signalType {
			continue
		}
		if strings.Contains(strings.ToLower(signal.Message), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func autoscalingScaleUpConfidencePending(collection autoscalingSignalCollection) bool {
	return collection.QueuePressure.PendingConfirmation || collection.CPU.PendingConfirmation
}

func formatObservedBytes(value int64, observed bool) string {
	if !observed {
		return "unknown"
	}
	return fmt.Sprintf("%d", value)
}

func summarizeAutoscalingSignals(signals []platformv1alpha1.AutoscalingSignalStatus) string {
	if len(signals) == 0 {
		return ""
	}

	parts := make([]string, 0, len(signals))
	for _, signal := range signals {
		message := strings.TrimSpace(signal.Message)
		if message == "" {
			message = "no details"
		}
		parts = append(parts, fmt.Sprintf("%s[%s]", signal.Type, message))
	}
	return strings.Join(parts, "; ")
}
