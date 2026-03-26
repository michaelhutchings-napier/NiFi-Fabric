package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/michaelhutchings-napier/NiFi-Fabric/internal/authztool"
)

type commandFunc func(context.Context, []string) error

func main() {
	ctx := context.Background()

	commands := map[string]commandFunc{
		"render":   runRender,
		"validate": runValidate,
		"diff":     runDiff,
	}

	if len(os.Args) < 2 {
		printRootUsage()
		os.Exit(1)
	}

	if os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help" {
		printRootUsage()
		return
	}

	cmd, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printRootUsage()
		os.Exit(1)
	}

	if err := cmd(ctx, os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printRootUsage() {
	fmt.Println(`nifi-fabric-authz renders and validates NiFi-Fabric OIDC authz overlays.

Usage:
  nifi-fabric-authz <command> [flags]

Commands:
  render     Render a deterministic values overlay
  validate   Validate config and optionally Keycloak and Helm compatibility
  diff       Show the diff between generated output and an existing file

Examples:
  nifi-fabric-authz render --config examples/oidc-keycloak-authz-config.yaml --output examples/oidc-keycloak-authz-values.yaml
  nifi-fabric-authz render --config examples/oidc-keycloak-authz-config.yaml --surface platform --output examples/platform-managed-oidc-keycloak-authz-values.yaml
  nifi-fabric-authz validate --config examples/oidc-keycloak-authz-config.yaml --helm --base-values examples/managed/values.yaml,examples/oidc-values.yaml
  NIFI_FABRIC_AUTHZ_KEYCLOAK_ACCESS_TOKEN=... nifi-fabric-authz validate --config examples/oidc-keycloak-authz-config.yaml --live`)
}

func runRender(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", "", "Path to the authz mapping config")
	outputPath := fs.String("output", "", "Path to write the generated values overlay")
	surfaceValue := fs.String("surface", string(authztool.SurfaceApp), "Output surface: app or platform")
	includeAuth := fs.Bool("include-auth-section", false, "Include auth.mode=oidc and auth.oidc.claims.groups in the output")
	checkMode := fs.Bool("check", false, "Check whether the output file is up to date instead of writing it")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}

	surface, err := authztool.ParseSurface(*surfaceValue)
	if err != nil {
		return err
	}

	cfg, err := authztool.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	rendered, err := authztool.RenderConfig(cfg, authztool.RenderOptions{
		Surface:            surface,
		IncludeAuthSection: *includeAuth,
		HeaderComment:      true,
	})
	if err != nil {
		return err
	}

	if *checkMode {
		if strings.TrimSpace(*outputPath) == "" {
			return errors.New("--output is required with --check")
		}
		current, err := os.ReadFile(*outputPath)
		if err != nil {
			return fmt.Errorf("read output file: %w", err)
		}
		if !bytes.Equal(current, rendered) {
			return fmt.Errorf("generated output differs from %s", *outputPath)
		}
		return nil
	}

	if strings.TrimSpace(*outputPath) == "" {
		_, err := os.Stdout.Write(rendered)
		return err
	}

	if err := os.WriteFile(*outputPath, rendered, 0o644); err != nil {
		return fmt.Errorf("write output file: %w", err)
	}
	return nil
}

func runValidate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", "", "Path to the authz mapping config")
	surfaceValue := fs.String("surface", string(authztool.SurfaceApp), "Output surface for Helm validation: app or platform")
	includeAuth := fs.Bool("include-auth-section", false, "Include auth.mode=oidc and auth.oidc.claims.groups when generating a Helm validation overlay")
	live := fs.Bool("live", false, "Validate live Keycloak groups using environment credentials")
	helmValidate := fs.Bool("helm", false, "Run helm template against the rendered overlay")
	helmChart := fs.String("helm-chart", "", "Optional chart path override for Helm validation")
	baseValues := fs.String("base-values", "", "Comma-separated base values files for Helm validation")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}

	surface, err := authztool.ParseSurface(*surfaceValue)
	if err != nil {
		return err
	}

	cfg, err := authztool.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	if *live {
		creds := authztool.Credentials{
			AccessToken: strings.TrimSpace(os.Getenv(authztool.EnvKeycloakAccessToken)),
		}
		if err := authztool.ValidateKeycloakMappings(ctx, cfg, creds); err != nil {
			return err
		}
	}

	if *helmValidate {
		rendered, err := authztool.RenderConfig(cfg, authztool.RenderOptions{
			Surface:            surface,
			IncludeAuthSection: *includeAuth,
			HeaderComment:      false,
		})
		if err != nil {
			return err
		}
		if err := authztool.ValidateWithHelm(ctx, rendered, surface, *helmChart, splitCSV(*baseValues)); err != nil {
			return err
		}
	}

	return nil
}

func runDiff(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	configPath := fs.String("config", "", "Path to the authz mapping config")
	againstPath := fs.String("against", "", "Existing values file to compare against")
	surfaceValue := fs.String("surface", string(authztool.SurfaceApp), "Output surface: app or platform")
	includeAuth := fs.Bool("include-auth-section", false, "Include auth.mode=oidc and auth.oidc.claims.groups in the generated output")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*configPath) == "" {
		return errors.New("--config is required")
	}
	if strings.TrimSpace(*againstPath) == "" {
		return errors.New("--against is required")
	}

	surface, err := authztool.ParseSurface(*surfaceValue)
	if err != nil {
		return err
	}

	cfg, err := authztool.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	rendered, err := authztool.RenderConfig(cfg, authztool.RenderOptions{
		Surface:            surface,
		IncludeAuthSection: *includeAuth,
		HeaderComment:      true,
	})
	if err != nil {
		return err
	}

	current, err := os.ReadFile(*againstPath)
	if err != nil {
		return fmt.Errorf("read compare target: %w", err)
	}
	if bytes.Equal(current, rendered) {
		return nil
	}

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(current)),
		B:        difflib.SplitLines(string(rendered)),
		FromFile: *againstPath,
		ToFile:   "generated",
		Context:  3,
	})
	if err != nil {
		return fmt.Errorf("build diff: %w", err)
	}
	fmt.Print(diff)
	return errors.New("generated output differs")
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
