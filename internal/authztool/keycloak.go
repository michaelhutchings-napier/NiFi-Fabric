package authztool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const (
	EnvKeycloakAccessToken = "NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN"
)

type Credentials struct {
	AccessToken string
}

type DiscoveryResult struct {
	NameValues     map[string]struct{}
	PathValues     map[string]struct{}
	DuplicateNames []string
	DuplicatePaths []string
}

type keycloakClient struct {
	baseURL    string
	realm      string
	httpClient *http.Client
	token      string
}

type keycloakGroup struct {
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	SubGroups []keycloakGroup `json:"subGroups"`
}

func DiscoverKeycloakGroups(ctx context.Context, cfg Config, creds Credentials) (DiscoveryResult, error) {
	client, err := newKeycloakClient(ctx, cfg.Source.BaseURL, cfg.Source.Realm, creds)
	if err != nil {
		return DiscoveryResult{}, err
	}

	groups, err := client.listGroups(ctx)
	if err != nil {
		return DiscoveryResult{}, err
	}

	nameCounts := map[string]int{}
	pathCounts := map[string]int{}
	flattenGroups(groups, "", nameCounts, pathCounts)

	return DiscoveryResult{
		NameValues:     countsToSet(nameCounts),
		PathValues:     countsToSet(pathCounts),
		DuplicateNames: countsToDuplicates(nameCounts),
		DuplicatePaths: countsToDuplicates(pathCounts),
	}, nil
}

func newKeycloakClient(ctx context.Context, baseURL, realm string, creds Credentials) (*keycloakClient, error) {
	token := strings.TrimSpace(creds.AccessToken)
	if token == "" {
		return nil, fmt.Errorf("live Keycloak validation requires %s", EnvKeycloakAccessToken)
	}

	return &keycloakClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		realm:      realm,
		httpClient: &http.Client{},
		token:      token,
	}, nil
}

func (c *keycloakClient) listGroups(ctx context.Context) ([]keycloakGroup, error) {
	const pageSize = 200

	var all []keycloakGroup
	for first := 0; ; first += pageSize {
		reqURL := fmt.Sprintf("%s/admin/realms/%s/groups?briefRepresentation=false&first=%d&max=%d", c.baseURL, url.PathEscape(c.realm), first, pageSize)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("build groups request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request groups: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read groups response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("request groups: unexpected status %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}

		var page []keycloakGroup
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode groups response: %w", err)
		}
		all = append(all, page...)
		if len(page) < pageSize {
			break
		}
	}

	return all, nil
}

func flattenGroups(groups []keycloakGroup, parentPath string, nameCounts, pathCounts map[string]int) {
	for _, group := range groups {
		path := group.Path
		if path == "" {
			if parentPath == "" {
				path = "/" + group.Name
			} else {
				path = parentPath + "/" + group.Name
			}
		}

		nameCounts[group.Name]++
		pathCounts[path]++
		flattenGroups(group.SubGroups, path, nameCounts, pathCounts)
	}
}

func countsToSet(counts map[string]int) map[string]struct{} {
	values := map[string]struct{}{}
	for value := range counts {
		values[value] = struct{}{}
	}
	return values
}

func countsToDuplicates(counts map[string]int) []string {
	duplicates := make([]string, 0)
	for value, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, value)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}
