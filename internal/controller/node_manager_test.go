package controller

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/michaelhutchings-napier/nifi-made-simple/api/v1alpha1"
	"github.com/michaelhutchings-napier/nifi-made-simple/internal/nifi"
)

func TestNormalizeNodeAddressStripsSchemeAndPort(t *testing.T) {
	tests := map[string]string{
		"https://nifi-2.nifi-headless.nifi.svc.cluster.local:8443": "nifi-2.nifi-headless.nifi.svc.cluster.local",
		"http://10.244.0.10:8443":                                  "10.244.0.10",
		"nifi-2.nifi-headless.nifi.svc.cluster.local:8443":         "nifi-2.nifi-headless.nifi.svc.cluster.local",
		"10.244.0.10": "10.244.0.10",
	}

	for input, expected := range tests {
		if actual := normalizeNodeAddress(input); actual != expected {
			t.Fatalf("normalizeNodeAddress(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestProgressDisconnectingRefreshesNodeStateAfterConflict(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr: &nifi.APIError{StatusCode: 409, Message: "node is not connected"},
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-1", Status: nifi.NodeStatusDisconnected},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-1",
		NodeID:    "node-1",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	result, err := manager.progressDisconnecting(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-1",
		Address: "nifi-1.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusConnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressDisconnecting returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected conflict refresh to be treated as ready once the node is already disconnected, got %+v", result)
	}
	if result.Message == "" {
		t.Fatalf("expected conflict refresh message")
	}
}

func TestProgressDisconnectingTreatsReplicationFailureForDisconnectedNodeAsReady(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr: &nifi.APIError{
				StatusCode: 500,
				Message:    "org.apache.nifi.web.client.api.WebClientServiceException: Request execution failed HTTP Method [PUT] URI [https://nifi-1.nifi-headless.nifi.svc.cluster.local:8443/nifi-api/controller/cluster/nodes/node-1]",
			},
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-1", Status: nifi.NodeStatusDisconnected},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeHibernation,
		PodName:   "nifi-1",
		NodeID:    "node-1",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	result, err := manager.progressDisconnecting(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-1",
		Address: "nifi-1.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusConnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressDisconnecting returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected replication failure with refreshed disconnected state to be treated as ready, got %+v", result)
	}
}

func TestProgressDisconnectingRequestsImmediateRequeueAfterDisconnectRequest(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-2", Status: nifi.NodeStatusDisconnected},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	result, err := manager.progressDisconnecting(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-2",
		Address: "nifi-2.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusConnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressDisconnecting returned error: %v", err)
	}
	if !result.RequeueNow {
		t.Fatalf("expected immediate requeue after requesting disconnect, got %+v", result)
	}
}

func TestProgressDisconnectingObservesNodeBecomingDisconnectedAndAdvancesToOffloading(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			getNodesResponses: [][]nifi.ClusterNode{
				{
					{NodeID: "node-2", Status: nifi.NodeStatusDisconnected},
				},
				{
					{NodeID: "node-2", Status: nifi.NodeStatusOffloaded},
				},
			},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	result, err := manager.progressDisconnecting(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-2",
		Address: "nifi-2.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusConnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressDisconnecting returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected observed disconnect to progress through offload readiness, got %+v", result)
	}
	updateStatuses := manager.NiFiClient.(*fakeNiFiClient).updateStatuses
	if len(updateStatuses) < 2 || updateStatuses[len(updateStatuses)-1] != nifi.NodeStatusOffloading {
		t.Fatalf("expected observed disconnect to request offload after disconnect, got %+v", updateStatuses)
	}
}

func TestPreparePodForOperationUsesNonTargetConnectedSourceNode(t *testing.T) {
	sts, pods, kubeClient := nodeManagerFixtures(t)
	fakeClient := &fakeNiFiClient{
		getNodesResponses: [][]nifi.ClusterNode{
			{
				{NodeID: "node-0", Address: "nifi-0.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
				{NodeID: "node-1", Address: "nifi-1.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
				{NodeID: "node-2", Address: "nifi-2.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
			},
		},
		getNodeResponses: []nifi.ClusterNode{
			{NodeID: "node-0", Address: "nifi-0.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
		},
	}
	manager := &LiveNodeManager{
		KubeClient: kubeClient,
		NiFiClient: fakeClient,
	}

	_, err := manager.PreparePodForOperation(context.Background(), nil, sts, pods, pods[0], platformv1alpha1.NodeOperationPurposeRestart, platformv1alpha1.NodeOperationStatus{}, 5*time.Minute)
	if err != nil {
		t.Fatalf("PreparePodForOperation returned error: %v", err)
	}
	if len(fakeClient.getNodesBaseURLs) == 0 {
		t.Fatalf("expected lifecycle source selection to query cluster nodes")
	}
	if got, want := fakeClient.getNodesBaseURLs[0], "https://nifi-1.nifi-headless.nifi.svc.cluster.local:8443"; got != want {
		t.Fatalf("expected first lifecycle source request to use non-target pod nifi-1, got %q want %q", got, want)
	}
	if len(fakeClient.updateBaseURLs) == 0 {
		t.Fatalf("expected lifecycle mutation to be issued")
	}
	if got, want := fakeClient.updateBaseURLs[0], "https://nifi-1.nifi-headless.nifi.svc.cluster.local:8443"; got != want {
		t.Fatalf("expected lifecycle mutation to use non-target pod nifi-1, got %q want %q", got, want)
	}
}

func TestPreparePodForOperationAdvancesFinalNodeWhenTargetAlreadyDisconnected(t *testing.T) {
	sts, pods, kubeClient := nodeManagerFixtures(t)
	fakeClient := &fakeNiFiClient{
		getNodesResponses: [][]nifi.ClusterNode{
			{
				{NodeID: "node-0", Address: "nifi-0.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusDisconnected},
				{NodeID: "node-1", Address: "nifi-1.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
				{NodeID: "node-2", Address: "nifi-2.nifi-headless.nifi.svc.cluster.local", Status: nifi.NodeStatusConnected},
			},
		},
		getNodeErr: &nifi.APIError{StatusCode: 409, Message: "direct node lookup should not be used"},
	}
	manager := &LiveNodeManager{
		KubeClient: kubeClient,
		NiFiClient: fakeClient,
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	current := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-0",
		PodUID:    string(pods[0].UID),
		NodeID:    "node-0",
		Stage:     platformv1alpha1.NodeOperationStageDisconnecting,
		StartedAt: &startedAt,
	}

	result, err := manager.PreparePodForOperation(context.Background(), nil, sts, pods, pods[0], platformv1alpha1.NodeOperationPurposeRestart, current, 5*time.Minute)
	if err != nil {
		t.Fatalf("PreparePodForOperation returned error: %v", err)
	}
	if result.Operation.Stage != platformv1alpha1.NodeOperationStageOffloading {
		t.Fatalf("expected already-disconnected final node to advance to offloading, got %+v", result.Operation)
	}
	if len(fakeClient.updateStatuses) == 0 || fakeClient.updateStatuses[0] != nifi.NodeStatusOffloading {
		t.Fatalf("expected final-node progression to request offload, got %v", fakeClient.updateStatuses)
	}
	if len(fakeClient.getNodeBaseURLs) != 0 {
		t.Fatalf("expected final-node state to come from control-plane cluster reads, got direct node lookups %v", fakeClient.getNodeBaseURLs)
	}
}

func TestProgressOffloadingTreatsConflictAsReadyWhenNodeAlreadyOffloaded(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr: &nifi.APIError{StatusCode: 409, Message: "node already offloaded"},
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-1", Status: nifi.NodeStatusOffloaded},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-1",
		NodeID:    "node-1",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-1",
		Address: "nifi-1.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected already-offloaded conflict to be treated as ready, got %+v", result)
	}
}

func TestProgressOffloadingTreatsConflictAsReadyWhenNodeIsAlreadyDisconnected(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr: &nifi.APIError{StatusCode: 409, Message: "node is not connected"},
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-0", Status: nifi.NodeStatusDisconnected},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-0",
		NodeID:    "node-0",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-0",
		Address: "nifi-0.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected disconnected node conflict to be treated as ready, got %+v", result)
	}
}

func TestProgressOffloadingObservesNodeBecomingOffloadedAfterRequest(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-2", Status: nifi.NodeStatusOffloaded},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-2",
		NodeID:    "node-2",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-2",
		Address: "nifi-2.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected offload request to observe readiness when the node becomes offloaded, got %+v", result)
	}
}

func TestProgressOffloadingKeepsOperationWhenConflictRefreshFails(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr:   &nifi.APIError{StatusCode: 409, Message: "node is not connected"},
			getNodesErr: &nifi.APIError{StatusCode: 503, Message: "cluster coordinator unavailable"},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-0",
		NodeID:    "node-0",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-0",
		Address: "nifi-0.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.RequeueNow {
		t.Fatalf("expected conflict refresh failure to keep retrying, got %+v", result)
	}
	if result.Operation.NodeID != "node-0" || result.Operation.Stage != platformv1alpha1.NodeOperationStageOffloading {
		t.Fatalf("expected in-flight node operation to be preserved, got %+v", result.Operation)
	}
}

func TestProgressOffloadingTreatsConflictRefreshNotConnectedAsReady(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr:   &nifi.APIError{StatusCode: 409, Message: "node is not connected"},
			getNodesErr: &nifi.APIError{StatusCode: 409, Message: "Cannot replicate request to Node nifi-0.nifi-headless.nifi.svc.cluster.local:8443 because the node is not connected"},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeRestart,
		PodName:   "nifi-0",
		NodeID:    "node-0",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-0",
		Address: "nifi-0.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected conflict refresh with not-connected response to be treated as ready, got %+v", result)
	}
}

func TestProgressOffloadingTreatsReplicationFailureForDisconnectedNodeAsReady(t *testing.T) {
	manager := &LiveNodeManager{
		NiFiClient: &fakeNiFiClient{
			updateErr: &nifi.APIError{
				StatusCode: 500,
				Message:    "org.apache.nifi.web.client.api.WebClientServiceException: Request execution failed HTTP Method [PUT] URI [https://nifi-1.nifi-headless.nifi.svc.cluster.local:8443/nifi-api/controller/cluster/nodes/node-1]",
			},
			getNodesResponses: [][]nifi.ClusterNode{{
				{NodeID: "node-1", Status: nifi.NodeStatusDisconnected},
			}},
		},
	}
	startedAt := metav1Time(time.Now().Add(-time.Minute))
	operation := platformv1alpha1.NodeOperationStatus{
		Purpose:   platformv1alpha1.NodeOperationPurposeHibernation,
		PodName:   "nifi-1",
		NodeID:    "node-1",
		Stage:     platformv1alpha1.NodeOperationStageOffloading,
		StartedAt: &startedAt,
	}

	result, err := manager.progressOffloading(context.Background(), nifi.APIRequest{}, nifi.ClusterNode{
		NodeID:  "node-1",
		Address: "nifi-1.nifi-headless.nifi.svc.cluster.local",
		Status:  nifi.NodeStatusDisconnected,
	}, operation, 5*time.Minute)
	if err != nil {
		t.Fatalf("progressOffloading returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected replication failure with refreshed disconnected state to be treated as ready, got %+v", result)
	}
}

type fakeNiFiClient struct {
	getNodesResponses [][]nifi.ClusterNode
	getNodesCalls     int
	getNodesBaseURLs  []string
	getNodesErr       error
	getNodesErrs      []error
	getNodeResponses  []nifi.ClusterNode
	getNodeCalls      int
	getNodeBaseURLs   []string
	getNodeErr        error
	updateBaseURLs    []string
	updateStatuses    []nifi.NodeStatus
	updateErr         error
}

func (f *fakeNiFiClient) GetClusterSummary(context.Context, nifi.ClusterSummaryRequest) (nifi.ClusterSummary, error) {
	return nifi.ClusterSummary{}, nil
}

func (f *fakeNiFiClient) GetNodes(_ context.Context, req nifi.APIRequest) ([]nifi.ClusterNode, error) {
	f.getNodesBaseURLs = append(f.getNodesBaseURLs, req.BaseURL)
	if len(f.getNodesErrs) > 0 {
		index := f.getNodesCalls
		if index >= len(f.getNodesErrs) {
			index = len(f.getNodesErrs) - 1
		}
		if f.getNodesErrs[index] != nil {
			f.getNodesCalls++
			return nil, f.getNodesErrs[index]
		}
	}
	if f.getNodesErr != nil {
		return nil, f.getNodesErr
	}
	if len(f.getNodesResponses) == 0 {
		return nil, nil
	}
	index := f.getNodesCalls
	if index >= len(f.getNodesResponses) {
		index = len(f.getNodesResponses) - 1
	}
	f.getNodesCalls++
	return f.getNodesResponses[index], nil
}

func (f *fakeNiFiClient) GetNode(_ context.Context, req nifi.APIRequest, _ string) (nifi.ClusterNode, error) {
	f.getNodeBaseURLs = append(f.getNodeBaseURLs, req.BaseURL)
	if f.getNodeErr != nil {
		return nifi.ClusterNode{}, f.getNodeErr
	}
	if len(f.getNodeResponses) == 0 {
		return nifi.ClusterNode{}, nil
	}
	index := f.getNodeCalls
	if index >= len(f.getNodeResponses) {
		index = len(f.getNodeResponses) - 1
	}
	f.getNodeCalls++
	return f.getNodeResponses[index], nil
}

func (f *fakeNiFiClient) UpdateNodeStatus(_ context.Context, req nifi.APIRequest, _ string, status nifi.NodeStatus) (nifi.ClusterNode, error) {
	f.updateBaseURLs = append(f.updateBaseURLs, req.BaseURL)
	f.updateStatuses = append(f.updateStatuses, status)
	if f.updateErr != nil {
		return nifi.ClusterNode{}, f.updateErr
	}
	return nifi.ClusterNode{}, nil
}

func nodeManagerFixtures(t *testing.T) (*appsv1.StatefulSet, []corev1.Pod, client.Client) {
	t.Helper()

	sts := managedStatefulSet("nifi", 3, "nifi-old", "nifi-new")
	sts.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{
		{
			Name: "SINGLE_USER_CREDENTIALS_USERNAME",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-auth"},
					Key:                  "username",
				},
			},
		},
		{
			Name: "SINGLE_USER_CREDENTIALS_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "nifi-auth"},
					Key:                  "password",
				},
			},
		},
	}
	pods := []corev1.Pod{
		readyPod("nifi-0", "nifi", "nifi-old"),
		readyPod("nifi-1", "nifi", "nifi-old"),
		readyPod("nifi-2", "nifi", "nifi-old"),
	}
	authSecret := watchedSecret("nifi-auth", corev1.SecretTypeOpaque, map[string][]byte{
		"username": []byte("admin"),
		"password": []byte("ChangeMeChangeMe1!"),
	})
	tlsSecret := watchedSecret("nifi-tls", corev1.SecretTypeOpaque, map[string][]byte{
		"ca.crt":             []byte("test-ca"),
		"keystorePassword":   []byte("changeit"),
		"truststorePassword": []byte("changeit"),
	})

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add client-go scheme: %v", err)
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(sts, &pods[0], &pods[1], &pods[2], authSecret, tlsSecret).
		Build()

	return sts, pods, kubeClient
}

func metav1Time(value time.Time) metav1.Time {
	return metav1.NewTime(value.UTC())
}
