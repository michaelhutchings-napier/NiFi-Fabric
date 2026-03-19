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
