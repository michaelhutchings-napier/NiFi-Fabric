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
	"strconv"
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

type RootProcessGroupStatus struct {
	FlowFilesQueued     int64
	BytesQueued         int64
	BytesQueuedObserved bool
}

type SystemDiagnostics struct {
	ActiveTimerDrivenThreads int32
	MaxTimerDrivenThreads    int32
	ThreadCountsObserved     bool
	CPULoadAverage           float64
	CPULoadObserved          bool
	AvailableProcessors      int32
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
	GetRootProcessGroupStatus(ctx context.Context, req APIRequest) (RootProcessGroupStatus, error)
	GetSystemDiagnostics(ctx context.Context, req APIRequest) (SystemDiagnostics, error)
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

func (c *HTTPClient) GetRootProcessGroupStatus(ctx context.Context, req APIRequest) (RootProcessGroupStatus, error) {
	httpClient, token, err := c.authorize(ctx, req)
	if err != nil {
		return RootProcessGroupStatus{}, err
	}

	request, err := c.newAuthenticatedRequest(ctx, httpClient, token, http.MethodGet, strings.TrimRight(req.BaseURL, "/")+"/nifi-api/flow/process-groups/root/status", nil)
	if err != nil {
		return RootProcessGroupStatus{}, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return RootProcessGroupStatus{}, fmt.Errorf("request root process-group status: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return RootProcessGroupStatus{}, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	var payload struct {
		ProcessGroupStatus struct {
			AggregateSnapshot map[string]json.RawMessage `json:"aggregateSnapshot"`
		} `json:"processGroupStatus"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return RootProcessGroupStatus{}, fmt.Errorf("decode root process-group status: %w", err)
	}

	flowFilesQueued, err := parseRawInt64(payload.ProcessGroupStatus.AggregateSnapshot["flowFilesQueued"])
	if err != nil {
		return RootProcessGroupStatus{}, fmt.Errorf("parse flowFilesQueued: %w", err)
	}

	status := RootProcessGroupStatus{FlowFilesQueued: flowFilesQueued}
	if bytesQueued, ok, err := parseOptionalRawBytes(payload.ProcessGroupStatus.AggregateSnapshot["bytesQueued"]); err != nil {
		return RootProcessGroupStatus{}, fmt.Errorf("parse bytesQueued: %w", err)
	} else if ok {
		status.BytesQueued = bytesQueued
		status.BytesQueuedObserved = true
	}

	return status, nil
}

func (c *HTTPClient) GetSystemDiagnostics(ctx context.Context, req APIRequest) (SystemDiagnostics, error) {
	httpClient, token, err := c.authorize(ctx, req)
	if err != nil {
		return SystemDiagnostics{}, err
	}

	request, err := c.newAuthenticatedRequest(ctx, httpClient, token, http.MethodGet, strings.TrimRight(req.BaseURL, "/")+"/nifi-api/system-diagnostics", nil)
	if err != nil {
		return SystemDiagnostics{}, err
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return SystemDiagnostics{}, fmt.Errorf("request system diagnostics: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return SystemDiagnostics{}, &APIError{
			StatusCode: response.StatusCode,
			Message:    strings.TrimSpace(string(body)),
		}
	}

	var payload struct {
		SystemDiagnostics struct {
			AggregateSnapshot map[string]json.RawMessage `json:"aggregateSnapshot"`
		} `json:"systemDiagnostics"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return SystemDiagnostics{}, fmt.Errorf("decode system diagnostics: %w", err)
	}

	status := SystemDiagnostics{}
	if active, ok, err := parseOptionalRawInt32(firstNonEmptyRawMessage(payload.SystemDiagnostics.AggregateSnapshot,
		"activeTimerDrivenThreadCount",
		"activeTimerDrivenThreads",
	)); err != nil {
		return SystemDiagnostics{}, fmt.Errorf("parse active timer-driven threads: %w", err)
	} else if ok {
		status.ActiveTimerDrivenThreads = active
	}
	if max, ok, err := parseOptionalRawInt32(firstNonEmptyRawMessage(payload.SystemDiagnostics.AggregateSnapshot,
		"maxTimerDrivenThreadCount",
		"maxTimerDrivenThreads",
	)); err != nil {
		return SystemDiagnostics{}, fmt.Errorf("parse max timer-driven threads: %w", err)
	} else if ok {
		status.MaxTimerDrivenThreads = max
	}
	status.ThreadCountsObserved = status.ActiveTimerDrivenThreads > 0 || status.MaxTimerDrivenThreads > 0

	if processors, ok, err := parseOptionalRawInt32(firstNonEmptyRawMessage(payload.SystemDiagnostics.AggregateSnapshot,
		"availableProcessors",
		"processorCount",
	)); err != nil {
		return SystemDiagnostics{}, fmt.Errorf("parse available processors: %w", err)
	} else if ok {
		status.AvailableProcessors = processors
	}
	if loadAverage, ok, err := parseOptionalRawFloat64(firstNonEmptyRawMessage(payload.SystemDiagnostics.AggregateSnapshot,
		"processorLoadAverage",
		"cpuLoadAverage",
	)); err != nil {
		return SystemDiagnostics{}, fmt.Errorf("parse CPU load average: %w", err)
	} else if ok && loadAverage >= 0 {
		status.CPULoadAverage = loadAverage
		status.CPULoadObserved = true
	}

	return status, nil
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

func firstNonEmptyRawMessage(values map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if value, ok := values[key]; ok && len(value) > 0 {
			return value
		}
	}
	return nil
}

func parseOptionalRawInt32(raw json.RawMessage) (int32, bool, error) {
	if len(raw) == 0 {
		return 0, false, nil
	}
	value, err := parseRawInt64(raw)
	if err != nil {
		return 0, false, err
	}
	return int32(value), true, nil
}

func parseOptionalRawFloat64(raw json.RawMessage) (float64, bool, error) {
	if len(raw) == 0 {
		return 0, false, nil
	}
	value, err := parseRawFloat64(raw)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func parseOptionalRawBytes(raw json.RawMessage) (int64, bool, error) {
	if len(raw) == 0 {
		return 0, false, nil
	}
	value, err := parseRawBytes(raw)
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func parseRawInt64(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("value was empty")
	}

	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("unsupported integer payload %q", string(raw))
	}

	token := normalizeNumericToken(strings.SplitN(text, "/", 2)[0])
	if token == "" {
		return 0, fmt.Errorf("no integer token found in %q", text)
	}
	value, err := strconv.ParseInt(token, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse integer token %q: %w", token, err)
	}
	return value, nil
}

func parseRawFloat64(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("value was empty")
	}

	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("unsupported float payload %q", string(raw))
	}

	token := extractFirstNumericToken(text)
	if token == "" {
		return 0, fmt.Errorf("no numeric token found in %q", text)
	}
	value, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float token %q: %w", token, err)
	}
	return value, nil
}

func parseRawBytes(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 {
		return 0, fmt.Errorf("value was empty")
	}

	var number int64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, fmt.Errorf("unsupported byte payload %q", string(raw))
	}

	return parseHumanBytes(text)
}

func parseHumanBytes(text string) (int64, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, fmt.Errorf("value was empty")
	}
	if strings.Contains(trimmed, "/") {
		parts := strings.SplitN(trimmed, "/", 2)
		trimmed = strings.TrimSpace(parts[len(parts)-1])
	}

	fields := strings.Fields(trimmed)
	switch len(fields) {
	case 0:
		return 0, fmt.Errorf("value was empty")
	case 1:
		number := extractFirstNumericToken(fields[0])
		if number == "" {
			return 0, fmt.Errorf("no numeric token found in %q", trimmed)
		}
		value, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return 0, fmt.Errorf("parse byte value %q: %w", number, err)
		}

		unit := extractUnitToken(fields[0])
		if unit == "" {
			return int64(value), nil
		}
		multiplier, ok := byteUnitMultiplier(unit)
		if !ok {
			return 0, fmt.Errorf("unsupported byte unit %q", unit)
		}
		return int64(value * multiplier), nil
	default:
		number := extractFirstNumericToken(fields[0])
		if number == "" {
			return 0, fmt.Errorf("no numeric token found in %q", trimmed)
		}
		value, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return 0, fmt.Errorf("parse byte value %q: %w", number, err)
		}

		multiplier, ok := byteUnitMultiplier(fields[1])
		if !ok {
			return 0, fmt.Errorf("unsupported byte unit %q", fields[1])
		}
		return int64(value * multiplier), nil
	}
}

func normalizeNumericToken(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), ",", "")
}

func extractFirstNumericToken(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case (r >= '0' && r <= '9') || r == '.' || r == '-' || r == ',':
			builder.WriteRune(r)
		case builder.Len() > 0:
			return normalizeNumericToken(builder.String())
		}
	}
	return normalizeNumericToken(builder.String())
}

func extractUnitToken(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			builder.WriteRune(r)
		}
	}
	return strings.ToUpper(builder.String())
}

func byteUnitMultiplier(unit string) (float64, bool) {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "B", "BYTE", "BYTES":
		return 1, true
	case "KB", "KIB":
		return 1024, true
	case "MB", "MIB":
		return 1024 * 1024, true
	case "GB", "GIB":
		return 1024 * 1024 * 1024, true
	case "TB", "TIB":
		return 1024 * 1024 * 1024 * 1024, true
	default:
		return 0, false
	}
}
