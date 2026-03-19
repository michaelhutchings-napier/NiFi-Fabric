package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesAmbientPodLabel(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "ambient.enabled=true",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Ambient enabled to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "istio.io/dataplane-mode: ambient") {
		t.Fatalf("expected rendered output to contain the Ambient pod label\n%s", output)
	}

	if strings.Contains(output, "sidecar.istio.io/inject") {
		t.Fatalf("did not expect sidecar annotations for the Ambient profile\n%s", output)
	}
}

func TestNiFiRenderAllowsAmbientLabelOverrides(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "ambient.enabled=true",
		"--set", "ambient.dataplaneMode=ambient",
		"--set-json", `ambient.labels={"example.mesh/profile":"bounded"}`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Ambient label overrides to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"istio.io/dataplane-mode: ambient",
		"example.mesh/profile: bounded",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedRenderIncludesNestedAmbientLabels(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-istio-ambient-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested Ambient values to succeed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "istio.io/dataplane-mode: ambient") {
		t.Fatalf("expected rendered platform output to contain the Ambient pod label\n%s", output)
	}
}

func TestNiFiRenderRejectsAmbientWithOtherMeshProfiles(t *testing.T) {
	for _, testCase := range [][]string{
		{"--set", "ambient.enabled=true", "--set", "linkerd.enabled=true"},
		{"--set", "ambient.enabled=true", "--set", "istio.enabled=true"},
	} {
		output, err := helmTemplate(t, "charts/nifi", testCase...)
		if err == nil {
			t.Fatalf("expected helm template to fail for conflicting mesh profiles\n%s", output)
		}
		if !strings.Contains(output, "choose one bounded service-mesh compatibility profile") {
			t.Fatalf("expected bounded mesh validation error\n%s", output)
		}
	}
}
