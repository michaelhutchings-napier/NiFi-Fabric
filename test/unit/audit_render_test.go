package unit

import (
	"strings"
	"testing"
)

func TestFlowActionAuditDisabledRendersNoAuditProperties(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi")
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, unexpected := range []string{
		"nifi.database.directory=./database_repository",
		"nifi.flow.configuration.archive.dir=",
		"nifi.web.request.log.format=",
	} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("expected rendered output to omit %q when flow-action audit is disabled\n%s", unexpected, output)
		}
	}
}

func TestFlowActionAuditLocalLayerRendersExpectedProperties(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
	)
	if err != nil {
		t.Fatalf("expected flow-action audit local layer to render: %v\n%s", err, output)
	}
	for _, want := range []string{
		"nifi.database.directory=./database_repository",
		"nifi.flow.configuration.archive.enabled=true",
		"nifi.flow.configuration.archive.dir=/opt/nifi/nifi-current/database_repository/flow-audit-archive",
		"nifi.flow.configuration.archive.max.time=30 days",
		"nifi.flow.configuration.archive.max.storage=2 GB",
		"nifi.flow.configuration.archive.max.count=1000",
		`nifi.web.request.log.format=%{client}a - %u %t "%r" %s %O "%{Referer}i" "%{User-Agent}i"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestFlowActionAuditValidationFailsWhenHistoryDisabled(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.local.history.enabled=false",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when flow-action audit disables local history\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.enabled=true currently requires local.history.enabled=true") {
		t.Fatalf("expected local history validation error\n%s", output)
	}
}

func TestFlowActionAuditValidationFailsForUnsupportedExportType(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.export.type=syslog",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported flow-action audit export type\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.export.type must be one of: disabled, log") {
		t.Fatalf("expected unsupported export-type validation error\n%s", output)
	}
}

func TestFlowActionAuditLogExportRendersReporterWiring(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.export.type=log",
		"--set", "image.tag=2.8.0",
		"--set", "observability.audit.flowActions.export.log.installation.image.repository=ghcr.io/example/nifi-flow-action-audit-reporter",
		"--set", "observability.audit.flowActions.export.log.installation.image.tag=0.0.1",
	)
	if err != nil {
		t.Fatalf("expected flow-action audit log export to render: %v\n%s", err, output)
	}
	for _, want := range []string{
		"nifi.nar.library.directory.flow.action.audit=/opt/nifi/fabric/extensions/flow-action-audit",
		"nifi.flow.action.reporter.implementation=io.nifi.fabric.audit.FlowActionJsonLogReporter",
		"name: install-flow-action-audit-reporter",
		"image: \"ghcr.io/example/nifi-flow-action-audit-reporter:0.0.1\"",
		"flow-action audit reporter NAR not found at /opt/nifi-fabric-audit/nifi-flow-action-audit-reporter.nar",
		"name: flow-action-audit-reporter",
		"mountPath: /opt/nifi/fabric/extensions/flow-action-audit",
		"readOnly: true",
		"emptyDir: {}",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestFlowActionAuditLogExportValidationFailsWithoutNarImage(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.export.type=log",
		"--set", "image.tag=2.8.0",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when log export lacks reporter image settings\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.export.log.installation.image.repository is required when export.type=log") {
		t.Fatalf("expected reporter image validation error\n%s", output)
	}
}

func TestPlatformManagedAuditFlowActionsExampleRendersReporterWiring(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"nifi.nar.library.directory.flow.action.audit=/opt/nifi/fabric/extensions/flow-action-audit",
		"nifi.flow.action.reporter.implementation=io.nifi.fabric.audit.FlowActionJsonLogReporter",
		"name: install-flow-action-audit-reporter",
		`image: "apache/nifi:2.8.0"`,
		`image: "nifi-flow-action-audit-reporter:0.0.1-SNAPSHOT"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedAuditFlowActionsLocalOnlyExampleOmitsReporterWiring(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-local-only-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"nifi.flow.configuration.archive.dir=/opt/nifi/nifi-current/database_repository/flow-audit-archive",
		"nifi.web.request.log.format=",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
	for _, unexpected := range []string{
		"nifi.flow.action.reporter.implementation=io.nifi.fabric.audit.FlowActionJsonLogReporter",
		"name: install-flow-action-audit-reporter",
		"name: flow-action-audit-reporter",
	} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("expected rendered output to omit %q\n%s", unexpected, output)
		}
	}
}

func TestPlatformManagedAuditFlowActionsPrivateRegistryExampleRendersExpectedImageSettings(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-private-registry-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`image: "registry.example.com/platform/nifi-fabric-flow-action-audit-reporter:0.1.0"`,
		"- name: internal-registry-creds",
		"nifi.flow.action.reporter.implementation=io.nifi.fabric.audit.FlowActionJsonLogReporter",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedAuditFlowActionsGhcrExampleRendersExpectedImageSettings(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-ghcr-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`image: "ghcr.io/example-org/nifi-fabric-flow-action-audit-reporter:0.1.0"`,
		"nifi.flow.action.reporter.implementation=io.nifi.fabric.audit.FlowActionJsonLogReporter",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedAuditFlowActionsKindOverlayKeepsSingleNodeShape(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-audit-flow-actions-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"replicas: 1",
		"nifi.flow.configuration.archive.dir=/opt/nifi/nifi-current/database_repository/flow-audit-archive",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
	if strings.Contains(output, `resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`) {
		t.Fatalf("expected kind audit overlay to avoid mutable-flow root-group bootstrap policy\n%s", output)
	}
}

func TestFlowActionAuditLogExportValidationFailsForUnsupportedNiFiVersion(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.export.type=log",
		"--set", "image.tag=2.0.0",
		"--set", "observability.audit.flowActions.export.log.installation.image.repository=ghcr.io/example/nifi-flow-action-audit-reporter",
		"--set", "observability.audit.flowActions.export.log.installation.image.tag=0.0.1",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when log export targets an unsupported NiFi version\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.export.type=log requires image.tag >= 2.4.0") {
		t.Fatalf("expected NiFi version validation error\n%s", output)
	}
}

func TestFlowActionAuditValidationFailsForInvalidPropertyValuesMode(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.content.propertyValues.mode=cleartext",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid flow-action audit propertyValues mode\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.content.propertyValues.mode must be redacted in the current supported implementation") {
		t.Fatalf("expected propertyValues mode validation error\n%s", output)
	}
}

func TestFlowActionAuditValidationFailsForAllowlistedPropertyValuesMode(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.content.propertyValues.mode=allowlisted",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported allowlisted propertyValues mode\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.content.propertyValues.mode must be redacted in the current supported implementation") {
		t.Fatalf("expected strict redaction validation error\n%s", output)
	}
}

func TestFlowActionAuditValidationFailsForInvalidArchiveCount(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.audit.flowActions.enabled=true",
		"--set", "observability.audit.flowActions.local.archive.retention.maxCount=0",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid flow-action audit archive maxCount\n%s", output)
	}
	if !strings.Contains(output, "observability.audit.flowActions.local.archive.retention.maxCount must be greater than zero") {
		t.Fatalf("expected archive maxCount validation error\n%s", output)
	}
}
