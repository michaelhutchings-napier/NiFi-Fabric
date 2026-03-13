package unit

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", ".."))
}

func helmTemplate(t *testing.T, chart string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"template", "test", chart}, args...)
	cmd := exec.Command("helm", cmdArgs...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestMetricsDisabledRendersNoMetricsResources(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi")
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "kind: ServiceMonitor") {
		t.Fatalf("expected no ServiceMonitor when metrics are disabled\n%s", output)
	}
	if strings.Contains(output, "app.kubernetes.io/component: metrics") {
		t.Fatalf("expected no dedicated metrics resources when metrics are disabled\n%s", output)
	}
}

func TestPlatformManagedNativeMetricsExampleRendersMultipleServiceMonitors(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-metrics-native-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	if got := strings.Count(output, "kind: ServiceMonitor"); got != 2 {
		t.Fatalf("expected 2 ServiceMonitors, got %d\n%s", got, output)
	}
	for _, want := range []string{
		"name: test-nifi-metrics",
		"name: test-nifi-flow",
		"name: test-nifi-flow-fast",
		"name: nifi-metrics-auth",
		"name: nifi-metrics-ca",
		"path: /nifi-api/flow/metrics/prometheus",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNativeMetricsAuthValidationFailsWithoutSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=basicAuth",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without a machine-auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.nativeApi.machineAuth.secretRef.name is required") {
		t.Fatalf("expected validation error for missing machine-auth Secret reference\n%s", output)
	}
}

func TestUnsupportedMetricsModeFailsClearly(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for exporter mode\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.mode=exporter is not implemented in this slice") {
		t.Fatalf("expected exporter-mode validation error\n%s", output)
	}
}
