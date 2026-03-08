package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
	"github.com/michaelhutchings-napier/nifi-made-simple/internal/nifi"
)

const defaultNodePreparationTimeout = 5 * time.Minute

type NodeManager interface {
	PreparePodForOperation(ctx context.Context, cluster *platformv1alpha1.NiFiCluster, sts *appsv1.StatefulSet, pods []corev1.Pod, pod corev1.Pod, purpose platformv1alpha1.NodeOperationPurpose, current platformv1alpha1.NodeOperationStatus, timeout time.Duration) (NodePreparationResult, error)
}

type NodePreparationResult struct {
	Ready     bool
	TimedOut  bool
	Message   string
	Operation platformv1alpha1.NodeOperationStatus
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

	managementPod, err := selectManagementPod(pods, pod.Name)
	if err != nil {
		return NodePreparationResult{}, err
	}
	apiRequest, err := m.apiRequest(ctx, sts, managementPod)
	if err != nil {
		return NodePreparationResult{}, err
	}

	operation := current
	if operation.PodName == "" || operation.PodName != pod.Name || operation.Purpose != purpose {
		node, err := m.resolvePodNode(ctx, apiRequest, sts, pod)
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

	node, err := m.NiFiClient.GetNode(ctx, apiRequest, operation.NodeID)
	if err != nil {
		return NodePreparationResult{}, err
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
			return NodePreparationResult{}, err
		}
		return NodePreparationResult{
			Message:   fmt.Sprintf("Requested NiFi disconnect for node %s", node.NodeID),
			Operation: operation,
		}, nil
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
			return NodePreparationResult{}, err
		}
		return NodePreparationResult{
			Message:   fmt.Sprintf("Requested NiFi offload for node %s", node.NodeID),
			Operation: operation,
		}, nil
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

func (m *LiveNodeManager) resolvePodNode(ctx context.Context, apiRequest nifi.APIRequest, sts *appsv1.StatefulSet, pod corev1.Pod) (nifi.ClusterNode, error) {
	nodes, err := m.NiFiClient.GetNodes(ctx, apiRequest)
	if err != nil {
		return nifi.ClusterNode{}, err
	}

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

func selectManagementPod(pods []corev1.Pod, targetPodName string) (*corev1.Pod, error) {
	ordered := append([]corev1.Pod(nil), pods...)
	sortPodsByOrdinal(ordered)

	for i := range ordered {
		if ordered[i].Name == targetPodName || ordered[i].DeletionTimestamp != nil || !isPodReady(&ordered[i]) {
			continue
		}
		return &ordered[i], nil
	}
	for i := range ordered {
		if ordered[i].DeletionTimestamp != nil || !isPodReady(&ordered[i]) {
			continue
		}
		return &ordered[i], nil
	}
	return nil, fmt.Errorf("no Ready management pod is available")
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
