package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
	"github.com/michaelhutchings-napier/nifi-made-simple/internal/nifi"
)

const (
	defaultNodePreparationTimeout          = 5 * time.Minute
	requestedDisconnectObservationWindow   = 10 * time.Second
	requestedDisconnectObservationInterval = 1 * time.Second
	requestedOffloadObservationWindow      = 10 * time.Second
	requestedOffloadObservationInterval    = 1 * time.Second
)

type NodeManager interface {
	PreparePodForOperation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose, current platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error)
}

type NodePreparationResult struct {
	Ready      bool
	TimedOut   bool
	RequeueNow bool
	Message    string
	Operation  platformv1alpha1.NodeOperationStatus
}

type LiveNodeManager struct {
	KubeClient client.Client
	NiFiClient nifi.Client
}

func (m *LiveNodeManager) PreparePodForOperation(ctx context.Context, _ *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose, current platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error) {
	if timeout <= 0 {
		timeout = defaultNodePreparationTimeout
	}
	if m.NiFiClient == nil {
		return NodePreparationResult{}, fmt.Errorf("NiFi client is not configured")
	}

	apiRequest, nodes, err := m.lifecycleAPIRequest(ctx, sts, pods, pod.Name)
	if err != nil {
		return NodePreparationResult{}, err
	}

	operation := current
	if operation.PodName == "" || operation.PodName != pod.Name || operation.Purpose != purpose {
		node, err := resolvePodNodeFromCluster(sts, pod, nodes)
		if err != nil {
			return NodePreparationResult{}, err
		}

		now := metav1Now()
		operation = platformv1alpha1.NodeOperationStatus{
			Purpose:   purpose,
			PodName:   pod.Name,
			PodUID:    string(pod.UID),
			NodeID:    node.NodeID,
			Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
			StartedAt: &now,
		}
	}
	if operation.Stage == "" {
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageDisconnecting
		operation.StartedAt = &now
	}

	node, err := m.nodeFromControlPlane(ctx, apiRequest, operation.NodeID, nodes)
	if err != nil {
		if disconnectedNode, ok := disconnectedNodeFromError(err, operation.NodeID); ok {
			node = disconnectedNode
		} else {
		return NodePreparationResult{}, err
		}
	}

	switch operation.Stage {
	case platformv1alpha1.NodeOperationStageDisconnecting:
		return m.progressDisconnecting(ctx, apiRequest, node, operation, timeout)
	case platformv1alpha1.NodeOperationStageOffloading:
		return m.progressOffloading(ctx, apiRequest, node, operation, timeout)
	default:
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageDisconnecting
		operation.StartedAt = &now
		return m.progressDisconnecting(ctx, apiRequest, node, operation, timeout)
	}
}

func (m *LiveNodeManager) progressDisconnecting(ctx context.Context, apiRequest nifi.APIRequest, node nifi.ClusterNode, operation platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error) {
	switch node.Status {
	case nifi.NodeStatusDisconnected:
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageOffloading
		operation.StartedAt = &now
		return m.progressOffloading(ctx, apiRequest, node, operation, timeout)
	case nifi.NodeStatusOffloading, nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageOffloading
		operation.StartedAt = &now
		return NodePreparationResult{
			Message:   fmt.Sprintf("Node %s is already %s; waiting for offload completion", node.NodeID, node.Status),
			Operation: operation,
		}, nil
	case nifi.NodeStatusDisconnecting:
		return NodePreparationResult{
			TimedOut:  nodeOperationTimedOut(operation, timeout),
			Message:   fmt.Sprintf("Waiting for NiFi node %s to reach DISCONNECTED before proceeding", node.NodeID),
			Operation: operation,
		}, nil
	default:
		if _, err := m.NiFiClient.UpdateNodeStatus(ctx, apiRequest, node.NodeID, nifi.NodeStatusDisconnecting); err != nil {
			if result, handled, conflictErr := m.nodePreparationConflictResult(ctx, apiRequest, operation, err, timeout); handled {
				return result, conflictErr
			}
			return NodePreparationResult{}, err
		}
		return m.observeDisconnectProgress(ctx, apiRequest, operation, timeout)
	}
}

func (m *LiveNodeManager) progressOffloading(ctx context.Context, apiRequest nifi.APIRequest, node nifi.ClusterNode, operation platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error) {
	switch node.Status {
	case nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
		return NodePreparationResult{
			Ready:   true,
			Message: fmt.Sprintf("NiFi node %s is %s and ready for %s", node.NodeID, node.Status, strings.ToLower(string(operation.Purpose))),
		}, nil
	case nifi.NodeStatusOffloading:
		return NodePreparationResult{
			TimedOut:  nodeOperationTimedOut(operation, timeout),
			Message:   fmt.Sprintf("Waiting for NiFi node %s to reach OFFLOADED before proceeding", node.NodeID),
			Operation: operation,
		}, nil
	case nifi.NodeStatusDisconnected:
		if _, err := m.NiFiClient.UpdateNodeStatus(ctx, apiRequest, node.NodeID, nifi.NodeStatusOffloading); err != nil {
			if result, handled, conflictErr := m.nodePreparationConflictResult(ctx, apiRequest, operation, err, timeout); handled {
				return result, conflictErr
			}
			return NodePreparationResult{}, err
		}
		return m.observeOffloadProgress(ctx, apiRequest, operation, timeout)
	case nifi.NodeStatusDisconnecting:
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageDisconnecting
		operation.StartedAt = &now
		return NodePreparationResult{
			Message:   fmt.Sprintf("NiFi node %s returned to DISCONNECTING; waiting for DISCONNECTED before offload", node.NodeID),
			Operation: operation,
		}, nil
	default:
		now := metav1Now()
		operation.Stage = platformv1alpha1.NodeOperationStageDisconnecting
		operation.StartedAt = &now
		return m.progressDisconnecting(ctx, apiRequest, node, operation, timeout)
	}
}

func (m *LiveNodeManager) nodePreparationConflictResult(ctx context.Context, apiRequest nifi.APIRequest, operation platformv1alpha1.NodeOperationStatus, err error, timeout time.Duration) (NodePreparationResult, bool, error) {
	var apiErr *nifi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		return NodePreparationResult{}, false, nil
	}

	refreshedNode, refreshErr := m.nodeFromControlPlane(ctx, apiRequest, operation.NodeID, nil)
	if refreshErr != nil {
		return NodePreparationResult{
			RequeueNow: true,
			Message:    fmt.Sprintf("Retrying NiFi node preparation for node %s after a conflict: %v", operation.NodeID, refreshErr),
			Operation:  operation,
		}, true, nil
	}

	switch operation.Stage {
	case platformv1alpha1.NodeOperationStageDisconnecting:
		switch refreshedNode.Status {
		case nifi.NodeStatusDisconnected:
			now := metav1Now()
			operation.Stage = platformv1alpha1.NodeOperationStageOffloading
			operation.StartedAt = &now
			result, progressErr := m.progressOffloading(ctx, apiRequest, refreshedNode, operation, timeout)
			return result, true, progressErr
		case nifi.NodeStatusOffloading, nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
			now := metav1Now()
			operation.Stage = platformv1alpha1.NodeOperationStageOffloading
			operation.StartedAt = &now
			return NodePreparationResult{
				Message:   fmt.Sprintf("NiFi reported a disconnect conflict for node %s; refreshed state is %s", refreshedNode.NodeID, refreshedNode.Status),
				Operation: operation,
			}, true, nil
		case nifi.NodeStatusDisconnecting:
			return NodePreparationResult{
				TimedOut:  nodeOperationTimedOut(operation, timeout),
				Message:   fmt.Sprintf("NiFi reported a disconnect conflict for node %s; refreshed state is DISCONNECTING", refreshedNode.NodeID),
				Operation: operation,
			}, true, nil
		default:
			return NodePreparationResult{
				Message:   fmt.Sprintf("NiFi reported a disconnect conflict for node %s; refreshed state is %s", refreshedNode.NodeID, refreshedNode.Status),
				Operation: operation,
			}, true, nil
		}
	case platformv1alpha1.NodeOperationStageOffloading:
		switch refreshedNode.Status {
		case nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
			return NodePreparationResult{
				Ready:   true,
				Message: fmt.Sprintf("NiFi node %s is %s and ready for %s", refreshedNode.NodeID, refreshedNode.Status, strings.ToLower(string(operation.Purpose))),
			}, true, nil
		case nifi.NodeStatusOffloading:
			return NodePreparationResult{
				TimedOut:  nodeOperationTimedOut(operation, timeout),
				Message:   fmt.Sprintf("NiFi reported an offload conflict for node %s; refreshed state is OFFLOADING", refreshedNode.NodeID),
				Operation: operation,
			}, true, nil
		case nifi.NodeStatusDisconnected:
			return NodePreparationResult{
				Ready:   true,
				Message: fmt.Sprintf("NiFi node %s is DISCONNECTED after an offload conflict and ready for %s", refreshedNode.NodeID, strings.ToLower(string(operation.Purpose))),
			}, true, nil
		default:
			now := metav1Now()
			operation.Stage = platformv1alpha1.NodeOperationStageDisconnecting
			operation.StartedAt = &now
			return NodePreparationResult{
				Message:   fmt.Sprintf("NiFi reported an offload conflict for node %s; refreshed state is %s", refreshedNode.NodeID, refreshedNode.Status),
				Operation: operation,
			}, true, nil
		}
	default:
		return NodePreparationResult{
			Message:   fmt.Sprintf("NiFi reported a node preparation conflict for node %s", refreshedNode.NodeID),
			Operation: operation,
		}, true, nil
	}
}

func (m *LiveNodeManager) observeDisconnectProgress(ctx context.Context, apiRequest nifi.APIRequest, operation platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error) {
	requestMessage := fmt.Sprintf("Requested NiFi disconnect for node %s", operation.NodeID)
	deadline := time.Now().Add(requestedDisconnectObservationWindow)

	for time.Now().Before(deadline) {
		if err := sleepWithContext(ctx, requestedDisconnectObservationInterval); err != nil {
			break
		}

		refreshedNode, err := m.nodeFromControlPlane(ctx, apiRequest, operation.NodeID, nil)
		if err != nil {
			if disconnectedNode, ok := disconnectedNodeFromError(err, operation.NodeID); ok {
				refreshedNode = disconnectedNode
			} else {
				return NodePreparationResult{}, err
			}
		}

		switch refreshedNode.Status {
		case nifi.NodeStatusDisconnected, nifi.NodeStatusOffloading, nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
			now := metav1Now()
			operation.Stage = platformv1alpha1.NodeOperationStageOffloading
			operation.StartedAt = &now
			return m.progressOffloading(ctx, apiRequest, refreshedNode, operation, timeout)
		case nifi.NodeStatusDisconnecting:
			continue
		}
	}

	return NodePreparationResult{
		RequeueNow: true,
		Message:    requestMessage,
		Operation:  operation,
	}, nil
}

func (m *LiveNodeManager) observeOffloadProgress(ctx context.Context, apiRequest nifi.APIRequest, operation platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error) {
	requestMessage := fmt.Sprintf("Requested NiFi offload for node %s", operation.NodeID)
	deadline := time.Now().Add(requestedOffloadObservationWindow)

	for time.Now().Before(deadline) {
		if err := sleepWithContext(ctx, requestedOffloadObservationInterval); err != nil {
			break
		}

		refreshedNode, err := m.nodeFromControlPlane(ctx, apiRequest, operation.NodeID, nil)
		if err != nil {
			if disconnectedNode, ok := disconnectedNodeFromError(err, operation.NodeID); ok {
				refreshedNode = disconnectedNode
			} else {
				return NodePreparationResult{}, err
			}
		}

		switch refreshedNode.Status {
		case nifi.NodeStatusOffloaded, nifi.NodeStatusRemoved:
			return NodePreparationResult{
				Ready:   true,
				Message: fmt.Sprintf("NiFi node %s is %s and ready for %s", refreshedNode.NodeID, refreshedNode.Status, strings.ToLower(string(operation.Purpose))),
			}, nil
		case nifi.NodeStatusOffloading:
			continue
		case nifi.NodeStatusDisconnected:
			continue
		}
	}

	return NodePreparationResult{
		RequeueNow: true,
		Message:    requestMessage,
		Operation:  operation,
	}, nil
}

func (m *LiveNodeManager) nodeFromControlPlane(ctx context.Context, apiRequest nifi.APIRequest, nodeID string, knownNodes []nifi.ClusterNode) (nifi.ClusterNode, error) {
	if node, ok := clusterNodeByID(knownNodes, nodeID); ok {
		return node, nil
	}

	nodes, err := m.NiFiClient.GetNodes(ctx, apiRequest)
	if err != nil {
		return nifi.ClusterNode{}, err
	}

	node, ok := clusterNodeByID(nodes, nodeID)
	if !ok {
		return nifi.ClusterNode{}, fmt.Errorf("could not find NiFi node %q in cluster control-plane view", nodeID)
	}
	return node, nil
}

func clusterNodeByID(nodes []nifi.ClusterNode, nodeID string) (nifi.ClusterNode, bool) {
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return node, true
		}
	}
	return nifi.ClusterNode{}, false
}

func disconnectedNodeFromError(err error, nodeID string) (nifi.ClusterNode, bool) {
	var apiErr *nifi.APIError
	if !errors.As(err, &apiErr) {
		return nifi.ClusterNode{}, false
	}
	if apiErr.StatusCode != http.StatusConflict {
		return nifi.ClusterNode{}, false
	}
	if !strings.Contains(strings.ToLower(apiErr.Message), "not connected") {
		return nifi.ClusterNode{}, false
	}
	return nifi.ClusterNode{
		NodeID:  nodeID,
		Status:  nifi.NodeStatusDisconnected,
		Address: nodeID,
	}, true
}

func resolvePodNodeFromCluster(sts *appsv1.StatefulSet, pod corev1.Pod, nodes []nifi.ClusterNode) (nifi.ClusterNode, error) {
	targetAddress := podDNSName(sts, &pod)
	targetHost := normalizeNodeAddress(targetAddress)
	targetIP := normalizeNodeAddress(pod.Status.PodIP)
	for _, node := range nodes {
		if normalizeNodeAddress(node.Address) == targetHost || normalizeNodeAddress(node.Address) == targetIP {
			return node, nil
		}
	}

	return nifi.ClusterNode{}, fmt.Errorf("could not match pod %q to a NiFi node using address %q", pod.Name, targetAddress)
}

func (m *LiveNodeManager) lifecycleAPIRequest(ctx context.Context, sts *appsv1.StatefulSet, pods []corev1.Pod, targetPodName string) (nifi.APIRequest, []nifi.ClusterNode, error) {
	ordered := append([]corev1.Pod(nil), pods...)
	sortPodsByOrdinal(ordered)

	var lastErr error
	for _, pod := range ordered {
		if pod.Name == targetPodName || pod.DeletionTimestamp != nil || !isPodReady(&pod) {
			continue
		}
		apiRequest, nodes, err := m.inspectLifecycleSource(ctx, sts, pod)
		if err != nil {
			lastErr = err
			continue
		}
		sourceNode, err := resolvePodNodeFromCluster(sts, pod, nodes)
		if err != nil {
			lastErr = err
			continue
		}
		if sourceNode.Status == nifi.NodeStatusConnected {
			return apiRequest, nodes, nil
		}
		lastErr = fmt.Errorf("candidate lifecycle source pod %q maps to NiFi node %q in state %s", pod.Name, sourceNode.NodeID, sourceNode.Status)
	}

	if lastErr != nil {
		return nifi.APIRequest{}, nil, lastErr
	}
	return nifi.APIRequest{}, nil, fmt.Errorf("no connected non-target pod is available for lifecycle control-plane calls")
}

func (m *LiveNodeManager) inspectLifecycleSource(ctx context.Context, sts *appsv1.StatefulSet, managementPod corev1.Pod) (nifi.APIRequest, []nifi.ClusterNode, error) {
	apiRequest, err := m.apiRequest(ctx, sts, &managementPod)
	if err != nil {
		return nifi.APIRequest{}, nil, err
	}
	nodes, err := m.NiFiClient.GetNodes(ctx, apiRequest)
	if err != nil {
		return nifi.APIRequest{}, nil, err
	}
	return apiRequest, nodes, nil
}

func (m *LiveNodeManager) apiRequest(ctx context.Context, sts *appsv1.StatefulSet, managementPod *corev1.Pod) (nifi.APIRequest, error) {
	auth, err := resolveAuthConfig(ctx, m.KubeClient, sts)
	if err != nil {
		return nifi.APIRequest{}, err
	}
	caCert, err := resolveCACert(ctx, m.KubeClient, sts)
	if err != nil {
		return nifi.APIRequest{}, err
	}

	return nifi.APIRequest{
		BaseURL:   podBaseURL(sts, managementPod),
		Username:  auth.Username,
		Password:  auth.Password,
		CACertPEM: caCert,
	}, nil
}

func nodeOperationTimedOut(operation platformv1alpha1.NodeOperationStatus, timeout time.Duration) bool {
	if operation.StartedAt == nil || timeout <= 0 {
		return false
	}
	return time.Since(operation.StartedAt.Time) > timeout
}

func podDNSName(sts *appsv1.StatefulSet, pod *corev1.Pod) string {
	return strings.TrimPrefix(strings.TrimPrefix(podBaseURL(sts, pod), "https://"), "http://")
}

func normalizeNodeAddress(address string) string {
	address = strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	host, _, found := strings.Cut(address, ":")
	if found {
		return host
	}
	return address
}

func metav1Now() metav1.Time {
	return metav1.NewTime(time.Now().UTC())
}
