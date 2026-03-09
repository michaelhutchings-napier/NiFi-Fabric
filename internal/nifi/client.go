package nifi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

type ClusterSummary struct {
	ConnectedNodeCount int32
	TotalNodeCount     int32
	ConnectedToCluster bool
	Clustered          bool
}

func (s ClusterSummary) Healthy(expectedReplicas int32) bool {
	return s.Clustered &&
		s.ConnectedToCluster &&
		s.ConnectedNodeCount == expectedReplicas &&
		s.TotalNodeCount == expectedReplicas
}

type ClusterSummaryRequest struct {
	BaseURL   string
	Username  string
	Password  string
	CACertPEM []byte
}

type APIRequest struct {
	BaseURL   string
	Username  string
	Password  string
	CACertPEM []byte
}

type NodeStatus string

const (
	NodeStatusConnecting    NodeStatus = "CONNECTING"
	NodeStatusConnected     NodeStatus = "CONNECTED"
	NodeStatusDisconnecting NodeStatus = "DISCONNECTING"
	NodeStatusDisconnected  NodeStatus = "DISCONNECTED"
	NodeStatusOffloading    NodeStatus = "OFFLOADING"
	NodeStatusOffloaded     NodeStatus = "OFFLOADED"
	NodeStatusRemoved       NodeStatus = "REMOVED"
	NodeStatusUnknown       NodeStatus = "UNKNOWN"
)

type ClusterNode struct {
	NodeID  string
	Address string
	APIPort int32
	Status  NodeStatus
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Message)
}

// Client is the minimal NiFi API surface needed for rollout health checks.
type Client interface {
	GetClusterSummary(ctx context.Context, req ClusterSummaryRequest) (ClusterSummary, error)
	GetNodes(ctx context.Context, req APIRequest) ([]ClusterNode, error)
	GetNode(ctx context.Context, req APIRequest, nodeID string) (ClusterNode, error)
	UpdateNodeStatus(ctx context.Context, req APIRequest, nodeID string, status NodeStatus) (ClusterNode, error)
}

type HTTPClient struct {
	Timeout time.Duration
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{Timeout: defaultTimeout}
}

func (c *HTTPClient) GetClusterSummary(ctx context.Context, req ClusterSummaryRequest) (ClusterSummary, error) {
	apiRequest := APIRequest(req)
	httpClient, err := c.newHTTPClient(apiRequest.CACertPEM)
	if err != nil {
		return ClusterSummary{}, err
	}

	token, err := c.requestToken(ctx, httpClient, apiRequest)
	if err != nil {
		return ClusterSummary{}, err
	}

	summaryURL := strings.TrimRight(apiRequest.BaseURL, "/") + "/nifi-api/flow/cluster/summary"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, summaryURL, nil)
	if err != nil {
		return ClusterSummary{}, fmt.Errorf("build cluster summary request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := httpClient.Do(request)
	if err != nil {
		return ClusterSummary{}, fmt.Errorf("request cluster summary: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ClusterSummary{}, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	var payload struct {
		ClusterSummary struct {
			ConnectedNodeCount int32 `json:"connectedNodeCount"`
			TotalNodeCount     int32 `json:"totalNodeCount"`
			ConnectedToCluster bool  `json:"connectedToCluster"`
			Clustered          bool  `json:"clustered"`
		} `json:"clusterSummary"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ClusterSummary{}, fmt.Errorf("decode cluster summary: %w", err)
	}

	return ClusterSummary{
		ConnectedNodeCount: payload.ClusterSummary.ConnectedNodeCount,
		TotalNodeCount:     payload.ClusterSummary.TotalNodeCount,
		ConnectedToCluster: payload.ClusterSummary.ConnectedToCluster,
		Clustered:          payload.ClusterSummary.Clustered,
	}, nil
}

func (c *HTTPClient) GetNodes(ctx context.Context, req APIRequest) ([]ClusterNode, error) {
	httpClient, token, err := c.authorize(ctx, req)
	if err != nil {
		return nil, err
	}

	request, err := c.newAuthenticatedRequest(ctx, httpClient, token, http.MethodGet, strings.TrimRight(req.BaseURL, "/")+"/nifi-api/controller/cluster", nil)
	if err != nil {
		return nil, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request cluster nodes: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	var payload struct {
		Cluster struct {
			Nodes []struct {
				NodeID  string `json:"nodeId"`
				Address string `json:"address"`
				APIPort int32  `json:"apiPort"`
				Status  string `json:"status"`
			} `json:"nodes"`
		} `json:"cluster"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode cluster nodes: %w", err)
	}

	nodes := make([]ClusterNode, 0, len(payload.Cluster.Nodes))
	for _, node := range payload.Cluster.Nodes {
		nodes = append(nodes, ClusterNode{
			NodeID:  node.NodeID,
			Address: node.Address,
			APIPort: node.APIPort,
			Status:  NodeStatus(node.Status),
		})
	}

	return nodes, nil
}

func (c *HTTPClient) GetNode(ctx context.Context, req APIRequest, nodeID string) (ClusterNode, error) {
	httpClient, token, err := c.authorize(ctx, req)
	if err != nil {
		return ClusterNode{}, err
	}

	request, err := c.newAuthenticatedRequest(ctx, httpClient, token, http.MethodGet, strings.TrimRight(req.BaseURL, "/")+"/nifi-api/controller/cluster/nodes/"+url.PathEscape(nodeID), nil)
	if err != nil {
		return ClusterNode{}, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return ClusterNode{}, fmt.Errorf("request cluster node: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ClusterNode{}, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	return decodeNode(response.Body)
}

func (c *HTTPClient) UpdateNodeStatus(ctx context.Context, req APIRequest, nodeID string, status NodeStatus) (ClusterNode, error) {
	httpClient, token, err := c.authorize(ctx, req)
	if err != nil {
		return ClusterNode{}, err
	}

	body := strings.NewReader(fmt.Sprintf(`{"node":{"nodeId":%q,"status":%q}}`, nodeID, string(status)))
	request, err := c.newAuthenticatedRequest(ctx, httpClient, token, http.MethodPut, strings.TrimRight(req.BaseURL, "/")+"/nifi-api/controller/cluster/nodes/"+url.PathEscape(nodeID), body)
	if err != nil {
		return ClusterNode{}, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := httpClient.Do(request)
	if err != nil {
		return ClusterNode{}, fmt.Errorf("update cluster node status: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return ClusterNode{}, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	return decodeNode(response.Body)
}

func decodeNode(reader io.Reader) (ClusterNode, error) {
	var payload struct {
		Node struct {
			NodeID  string `json:"nodeId"`
			Address string `json:"address"`
			APIPort int32  `json:"apiPort"`
			Status  string `json:"status"`
		} `json:"node"`
	}
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		return ClusterNode{}, fmt.Errorf("decode cluster node: %w", err)
	}

	return ClusterNode{
		NodeID:  payload.Node.NodeID,
		Address: payload.Node.Address,
		APIPort: payload.Node.APIPort,
		Status:  NodeStatus(payload.Node.Status),
	}, nil
}

func (c *HTTPClient) authorize(ctx context.Context, req APIRequest) (*http.Client, string, error) {
	httpClient, err := c.newHTTPClient(req.CACertPEM)
	if err != nil {
		return nil, "", err
	}

	token, err := c.requestToken(ctx, httpClient, req)
	if err != nil {
		return nil, "", err
	}

	return httpClient, token, nil
}

func (c *HTTPClient) newAuthenticatedRequest(ctx context.Context, _ *http.Client, token, method, targetURL string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("build request %s %s: %w", method, targetURL, err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return request, nil
}

func (c *HTTPClient) requestToken(ctx context.Context, httpClient *http.Client, req APIRequest) (string, error) {
	form := url.Values{}
	form.Set("username", req.Username)
	form.Set("password", req.Password)

	tokenURL := strings.TrimRight(req.BaseURL, "/") + "/nifi-api/access/token"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build access token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	response, err := httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request access token: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return "", &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	tokenBytes, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil {
		return "", fmt.Errorf("read access token response: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("access token response was empty")
	}

	return token, nil
}

func (c *HTTPClient) newHTTPClient(caCertPEM []byte) (*http.Client, error) {
	if len(caCertPEM) == 0 {
		return nil, fmt.Errorf("CA certificate is required")
	}

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("append CA certificate to trust pool")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    rootCAs,
			},
		},
	}, nil
}
