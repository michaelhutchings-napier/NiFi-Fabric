package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesIstioPodAnnotations(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "istio.enabled=true",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Istio enabled to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"sidecar.istio.io/inject: \"true\"",
		"sidecar.istio.io/rewriteAppHTTPProbers: \"true\"",
		"proxy.istio.io/config: '{\"holdApplicationUntilProxyStarts\":true}'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderAllowsIstioAnnotationOverrides(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "istio.enabled=true",
		"--set", "istio.rewriteAppHTTPProbers=false",
		"--set", "istio.holdApplicationUntilProxyStarts=false",
		"--set-json", `istio.annotations={"traffic.sidecar.istio.io/includeInboundPorts":"8443,11443,6342"}`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Istio annotation overrides to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"sidecar.istio.io/inject: \"true\"",
		"sidecar.istio.io/rewriteAppHTTPProbers: \"false\"",
		"traffic.sidecar.istio.io/includeInboundPorts: 8443,11443,6342",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}

	if strings.Contains(output, "proxy.istio.io/config") {
		t.Fatalf("did not expect proxy.istio.io/config when holdApplicationUntilProxyStarts=false\n%s", output)
	}
}

func TestPlatformManagedRenderIncludesNestedIstioAnnotations(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-istio-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested Istio values to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"sidecar.istio.io/inject: \"true\"",
		"sidecar.istio.io/rewriteAppHTTPProbers: \"true\"",
		"proxy.istio.io/config: '{\"holdApplicationUntilProxyStarts\":true}'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderRejectsMultipleMeshProfiles(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "linkerd.enabled=true",
		"--set", "istio.enabled=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when both Linkerd and Istio are enabled\n%s", output)
	}
	if !strings.Contains(output, "choose one bounded service-mesh compatibility profile") {
		t.Fatalf("expected bounded mesh validation error\n%s", output)
	}
}
