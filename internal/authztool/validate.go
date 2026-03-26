package authztool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ValidateKeycloakMappings(ctx context.Context, cfg Config, creds Credentials) error {
	discovery, err := DiscoverKeycloakGroups(ctx, cfg, creds)
	if err != nil {
		return err
	}

	var problems Problems
	activeValues := discovery.NameValues
	otherValues := discovery.PathValues
	activeDuplicates := discovery.DuplicateNames
	otherMode := GroupValueModePath
	if cfg.Source.GroupValueMode == GroupValueModePath {
		activeValues = discovery.PathValues
		otherValues = discovery.NameValues
		activeDuplicates = discovery.DuplicatePaths
		otherMode = GroupValueModeName
	}

	for _, duplicate := range activeDuplicates {
		problems.Addf("discovered duplicate Keycloak group value %q in %s mode; choose a less ambiguous groupValueMode or rename groups", duplicate, cfg.Source.GroupValueMode)
	}

	validateExpectedGroup := func(fieldName, value string) {
		if _, ok := activeValues[value]; ok {
			return
		}
		if _, ok := otherValues[value]; ok {
			problems.Addf("%s %q exists in Keycloak but only matches the %s claim shape; configured groupValueMode=%q does not line up with emitted claim values", fieldName, value, otherMode, cfg.Source.GroupValueMode)
			return
		}
		problems.Addf("%s %q was not found in Keycloak for configured groupValueMode=%q", fieldName, value, cfg.Source.GroupValueMode)
	}

	validateExpectedGroup("authz.initialAdminGroup", cfg.Authz.InitialAdminGroup)
	for _, binding := range cfg.Bindings {
		validateExpectedGroup("mapped Keycloak group", binding.KeycloakGroup)
	}

	return problems.Err()
}

func ValidateWithHelm(ctx context.Context, rendered []byte, surface Surface, chartPath string, baseValues []string) error {
	if chartPath == "" {
		switch surface {
		case SurfaceApp:
			chartPath = "charts/nifi"
		case SurfacePlatform:
			chartPath = "charts/nifi-platform"
		default:
			return fmt.Errorf("unsupported surface %q", surface)
		}
	}

	tmpDir, err := os.MkdirTemp("", "nifi-fabric-authz-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	renderedPath := filepath.Join(tmpDir, "generated-values.yaml")
	if err := os.WriteFile(renderedPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write rendered values: %w", err)
	}

	args := []string{"template", "nifi-fabric-authz", chartPath}
	for _, base := range baseValues {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		args = append(args, "-f", base)
	}
	args = append(args, "-f", renderedPath)

	cmd := exec.CommandContext(ctx, "helm", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm template failed: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	return nil
}
