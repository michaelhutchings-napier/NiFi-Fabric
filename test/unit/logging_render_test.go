package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesLoggingLevelsConfigAndBootstrapPatch(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set-json", `logging.levels={"org.apache.nifi":"debug","org.apache.nifi.web.api":"TRACE"}`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with logging levels to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"logging-levels.properties: |",
		"org.apache.nifi=DEBUG",
		"org.apache.nifi.web.api=TRACE",
		"apply_logging_levels /config/logging-levels.properties /work-conf/logback.xml",
		`python_script="/tmp/apply-logging-levels.py"`,
		`printf '%s\n' \`,
		`logger = ET.Element("logger", {"name": name, "level": level})`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderFailsForUnsupportedLoggingLevel(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "logging.levels.org\\.apache\\.nifi=NOTICE",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported logging level\n%s", output)
	}
	if !strings.Contains(output, `logging.levels["org.apache.nifi"] must be one of: TRACE, DEBUG, INFO, WARN, ERROR`) {
		t.Fatalf("expected unsupported logging level validation error\n%s", output)
	}
}

func TestNiFiRenderFailsForRootLoggingLevel(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "logging.levels.root=DEBUG",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for root logger overrides\n%s", output)
	}
	if !strings.Contains(output, "logging.levels[root] is not supported; configure named loggers only") {
		t.Fatalf("expected root logger validation error\n%s", output)
	}
}

func TestPlatformManagedRenderIncludesNestedLoggingLevels(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set-json", `nifi.logging.levels={"org.apache.nifi":"DEBUG"}`,
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested logging levels to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"logging-levels.properties: |",
		"org.apache.nifi=DEBUG",
		"apply_logging_levels /config/logging-levels.properties /work-conf/logback.xml",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
