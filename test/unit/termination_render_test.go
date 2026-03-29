package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesTerminationGracePeriodSeconds(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi")
	if err != nil {
		t.Fatalf("expected nifi chart render to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "terminationGracePeriodSeconds: 120") {
		t.Fatalf("expected rendered output to contain default termination grace period\n%s", output)
	}
}

func TestNiFiRenderAllowsOverridingTerminationGracePeriodSeconds(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi", "--set", "terminationGracePeriodSeconds=180")
	if err != nil {
		t.Fatalf("expected nifi chart render with override to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "terminationGracePeriodSeconds: 180") {
		t.Fatalf("expected rendered output to contain overridden termination grace period\n%s", output)
	}
}

func TestNiFiRenderIncludesGracefulTerminationHooks(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi")
	if err != nil {
		t.Fatalf("expected nifi chart render to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "trap 'stop_nifi; wait \"${nifi_pid}\" 2>/dev/null || true; wait \"${tail_pid}\" 2>/dev/null || true; exit 0' TERM INT") {
		t.Fatalf("expected rendered output to include the graceful termination trap\n%s", output)
	}
	if strings.Contains(output, "preStop:") {
		t.Fatalf("expected rendered output to keep termination handling in the container entrypoint, not an extra preStop hook\n%s", output)
	}
}
