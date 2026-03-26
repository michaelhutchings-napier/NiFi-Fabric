package authztool

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SourceTypeKeycloak = "keycloak"

	GroupValueModeName = "name"
	GroupValueModePath = "path"
)

var allowedBundles = []string{
	"viewer",
	"editor",
	"flowVersionManager",
	"admin",
}

type Config struct {
	Source   SourceConfig `yaml:"source"`
	Authz    AuthzConfig  `yaml:"authz"`
	Bindings []Binding    `yaml:"bindings"`
}

type SourceConfig struct {
	Type           string `yaml:"type"`
	BaseURL        string `yaml:"baseUrl"`
	Realm          string `yaml:"realm"`
	GroupsClaim    string `yaml:"groupsClaim"`
	GroupValueMode string `yaml:"groupValueMode"`
}

type AuthzConfig struct {
	InitialAdminGroup string `yaml:"initialAdminGroup"`
}

type Binding struct {
	KeycloakGroup string   `yaml:"keycloakGroup"`
	Bundles       []string `yaml:"bundles"`
}

type Problems []string

func (p *Problems) Addf(format string, args ...any) {
	*p = append(*p, fmt.Sprintf(format, args...))
}

func (p Problems) Err() error {
	if len(p) == 0 {
		return nil
	}
	return fmt.Errorf("validation failed:\n- %s", strings.Join(p, "\n- "))
}

func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}

	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) normalize() {
	c.Source.Type = strings.TrimSpace(c.Source.Type)
	c.Source.BaseURL = strings.TrimRight(strings.TrimSpace(c.Source.BaseURL), "/")
	c.Source.Realm = strings.TrimSpace(c.Source.Realm)
	c.Source.GroupsClaim = strings.TrimSpace(c.Source.GroupsClaim)
	c.Source.GroupValueMode = strings.TrimSpace(c.Source.GroupValueMode)
	if c.Source.GroupValueMode == "" {
		c.Source.GroupValueMode = GroupValueModeName
	}

	c.Authz.InitialAdminGroup = strings.TrimSpace(c.Authz.InitialAdminGroup)

	for i := range c.Bindings {
		c.Bindings[i].KeycloakGroup = strings.TrimSpace(c.Bindings[i].KeycloakGroup)
		for j := range c.Bindings[i].Bundles {
			c.Bindings[i].Bundles[j] = strings.TrimSpace(c.Bindings[i].Bundles[j])
		}
		sort.Strings(c.Bindings[i].Bundles)
	}
}

func (c Config) Validate() error {
	var problems Problems

	if c.Source.Type != SourceTypeKeycloak {
		problems.Addf("source.type must be %q", SourceTypeKeycloak)
	}
	if c.Source.BaseURL == "" {
		problems.Addf("source.baseUrl is required")
	}
	if c.Source.Realm == "" {
		problems.Addf("source.realm is required")
	}
	if c.Source.GroupsClaim == "" {
		problems.Addf("source.groupsClaim is required")
	}
	if c.Source.GroupValueMode != GroupValueModeName && c.Source.GroupValueMode != GroupValueModePath {
		problems.Addf("source.groupValueMode must be %q or %q", GroupValueModeName, GroupValueModePath)
	}
	if c.Authz.InitialAdminGroup == "" {
		problems.Addf("authz.initialAdminGroup is required")
	}
	if len(c.Bindings) == 0 {
		problems.Addf("bindings must contain at least one group-to-bundle mapping")
	}

	seenGroups := map[string]struct{}{}
	allowed := map[string]struct{}{}
	for _, bundle := range allowedBundles {
		allowed[bundle] = struct{}{}
	}

	for i, binding := range c.Bindings {
		if binding.KeycloakGroup == "" {
			problems.Addf("bindings[%d].keycloakGroup is required", i)
		}
		if _, ok := seenGroups[binding.KeycloakGroup]; ok {
			problems.Addf("bindings[%d].keycloakGroup %q is duplicated; use one binding per group", i, binding.KeycloakGroup)
		}
		seenGroups[binding.KeycloakGroup] = struct{}{}

		if len(binding.Bundles) == 0 {
			problems.Addf("bindings[%d].bundles must contain at least one bundle", i)
			continue
		}

		seenBundles := map[string]struct{}{}
		for j, bundle := range binding.Bundles {
			if bundle == "" {
				problems.Addf("bindings[%d].bundles[%d] must not be empty", i, j)
				continue
			}
			if _, ok := allowed[bundle]; !ok {
				problems.Addf("bindings[%d].bundles[%d] %q is not supported", i, j, bundle)
			}
			if _, ok := seenBundles[bundle]; ok {
				problems.Addf("bindings[%d].bundles contains duplicate bundle %q", i, bundle)
			}
			seenBundles[bundle] = struct{}{}
		}
	}

	return problems.Err()
}

func (c Config) DerivedApplicationGroups() []string {
	set := map[string]struct{}{
		c.Authz.InitialAdminGroup: {},
	}
	for _, binding := range c.Bindings {
		set[binding.KeycloakGroup] = struct{}{}
	}

	groups := make([]string, 0, len(set))
	for group := range set {
		if strings.TrimSpace(group) == "" {
			continue
		}
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}

func (c Config) BundleGroups() map[string][]string {
	result := map[string][]string{}
	for _, bundle := range allowedBundles {
		result[bundle] = nil
	}

	tmp := map[string]map[string]struct{}{}
	for _, bundle := range allowedBundles {
		tmp[bundle] = map[string]struct{}{}
	}

	for _, binding := range c.Bindings {
		for _, bundle := range binding.Bundles {
			tmp[bundle][binding.KeycloakGroup] = struct{}{}
		}
	}

	for _, bundle := range allowedBundles {
		groups := make([]string, 0, len(tmp[bundle]))
		for group := range tmp[bundle] {
			groups = append(groups, group)
		}
		sort.Strings(groups)
		result[bundle] = groups
	}

	return result
}
