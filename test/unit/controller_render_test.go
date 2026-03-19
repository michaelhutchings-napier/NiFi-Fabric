package unit

import (
	"strings"
	"testing"
)

func TestPlatformManagedControllerRenderIncludesSecurityDefaults(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with managed controller to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Deployment",
		"app.kubernetes.io/component: controller",
		"automountServiceAccountToken: true",
		"enableServiceLinks: false",
		"fsGroup: 65532",
		"runAsUser: 65532",
		"runAsGroup: 65532",
		"runAsNonRoot: true",
		"allowPrivilegeEscalation: false",
		"drop:",
		"- ALL",
		"seccompProfile:",
		"type: RuntimeDefault",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedControllerRenderAllowsSecurityOverrides(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set-json", `controller.imagePullSecrets=[{"name":"controller-registry-creds"}]`,
		"--set", "controller.automountServiceAccountToken=false",
		"--set", "controller.enableServiceLinks=true",
		"--set", "controller.podSecurityContext.fsGroup=7777",
		"--set", "controller.securityContext.runAsUser=7777",
		"--set", "controller.securityContext.runAsGroup=7777",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with controller security overrides to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: controller-registry-creds",
		"automountServiceAccountToken: false",
		"enableServiceLinks: true",
		"fsGroup: 7777",
		"runAsUser: 7777",
		"runAsGroup: 7777",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}
