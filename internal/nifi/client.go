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

// Client is the minimal NiFi API surface needed for rollout health checks.
type Client interface {
	GetClusterSummary(ctx context.Context, req ClusterSummaryRequest) (ClusterSummary, error)
}

type HTTPClient struct {
	Timeout time.Duration
}

func NewHTTPClient() *HTTPClient {
	return &HTTPClient{Timeout: defaultTimeout}
}

func (c *HTTPClient) GetClusterSummary(ctx context.Context, req ClusterSummaryRequest) (ClusterSummary, error) {
	httpClient, err := c.newHTTPClient(req.CACertPEM)
	if err != nil {
		return ClusterSummary{}, err
	}

	token, err := c.requestToken(ctx, httpClient, req)
	if err != nil {
		return ClusterSummary{}, err
	}

	summaryURL := strings.TrimRight(req.BaseURL, "/") + "/nifi-api/flow/cluster/summary"
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
		return ClusterSummary{}, fmt.Errorf("cluster summary returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
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

func (c *HTTPClient) requestToken(ctx context.Context, httpClient *http.Client, req ClusterSummaryRequest) (string, error) {
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
		return "", fmt.Errorf("access token returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
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
