package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesLinkerdPodAndHeadlessAnnotations(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "linkerd.enabled=true",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Linkerd enabled to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"linkerd.io/inject: enabled",
		"config.linkerd.io/opaque-ports: 11443,6342",
		"name: test-nifi-headless",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}

	if strings.Contains(output, "name: test-nifi-metrics\n  annotations:\n    config.linkerd.io/opaque-ports") {
		t.Fatalf("did not expect the native https metrics service to be marked opaque by the default bounded Linkerd profile\n%s", output)
	}
}

func TestNiFiRenderAllowsLinkerdHTTPSOpaqueOverride(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "linkerd.enabled=true",
		"--set", "linkerd.opaquePorts.https=true",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=basicAuth",
		"--set", "observability.metrics.nativeApi.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.nativeApi.endpoints[0].name=flow",
		"--set", "observability.metrics.nativeApi.endpoints[0].enabled=true",
		"--set", "observability.metrics.nativeApi.endpoints[0].path=/nifi-api/flow/metrics/prometheus",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with Linkerd https opaque override to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"config.linkerd.io/opaque-ports: 8443,11443,6342",
		"name: test-nifi-metrics",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedRenderIncludesNestedLinkerdAnnotations(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-linkerd-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested Linkerd values to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"linkerd.io/inject: enabled",
		"config.linkerd.io/opaque-ports: 11443,6342",
		"name: test-nifi-headless",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
