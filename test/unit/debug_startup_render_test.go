package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesDebugStartupPauseAndDisablesProbes(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "debugStartup.enabled=true",
		"--set", "debugStartup.sleepSeconds=1800",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with debugStartup to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`debug_startup_seconds="1800"`,
		`debugStartup.enabled=true: pausing ${debug_startup_seconds}s before NiFi startup for operator inspection`,
		`debugStartup.enabled=true: startup and liveness probes are disabled while readiness continues to hold the pod out of service`,
		`sleep "${debug_startup_seconds}"`,
		"readinessProbe:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
	for _, blocked := range []string{"startupProbe:", "livenessProbe:"} {
		if strings.Contains(output, blocked) {
			t.Fatalf("did not expect %s when debugStartup is enabled\n%s", blocked, output)
		}
	}
}

func TestNiFiRenderFailsForNonPositiveDebugStartupSleep(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "debugStartup.enabled=true",
		"--set", "debugStartup.sleepSeconds=0",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for non-positive debugStartup.sleepSeconds\n%s", output)
	}
	if !strings.Contains(output, "debugStartup.sleepSeconds must be greater than zero when debugStartup.enabled=true") {
		t.Fatalf("expected debugStartup validation error\n%s", output)
	}
}

func TestPlatformManagedRenderIncludesNestedDebugStartupPause(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set", "nifi.debugStartup.enabled=true",
		"--set", "nifi.debugStartup.sleepSeconds=900",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested debugStartup to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`debug_startup_seconds="900"`,
		`debugStartup.enabled=true: pausing ${debug_startup_seconds}s before NiFi startup for operator inspection`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
