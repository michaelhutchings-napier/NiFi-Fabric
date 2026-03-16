package unit

import (
	"strings"
	"testing"
)

func TestParameterContextsValidationFailsWithoutContexts(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "parameterContexts.enabled=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when parameterContexts is enabled without any contexts\n%s", output)
	}
	if !strings.Contains(output, "parameterContexts.enabled=true requires parameterContexts.contexts to contain at least one context definition") {
		t.Fatalf("expected missing contexts validation error\n%s", output)
	}
}

func TestParameterContextsValidationFailsForSensitiveInlineValue(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=runtime",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.token",
		"--set", "parameterContexts.contexts[0].parameters[0].sensitive=true",
		"--set", "parameterContexts.contexts[0].parameters[0].value=inline-secret",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when a sensitive parameter uses an inline value\n%s", output)
	}
	if !strings.Contains(output, "parameterContexts.contexts[0].parameters[0] must use secretRef when sensitive=true") {
		t.Fatalf("expected sensitive parameter validation error\n%s", output)
	}
}

func TestParameterContextsValidationFailsForMixedValueSources(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=runtime",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.token",
		"--set", "parameterContexts.contexts[0].parameters[0].sensitive=true",
		"--set", "parameterContexts.contexts[0].parameters[0].value=inline-secret",
		"--set", "parameterContexts.contexts[0].parameters[0].secretRef.name=parameter-context",
		"--set", "parameterContexts.contexts[0].parameters[0].secretRef.key=api-token",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when a parameter mixes inline and secretRef sources\n%s", output)
	}
	if !strings.Contains(output, "parameterContexts.contexts[0].parameters[0] supports either value or secretRef, not both") {
		t.Fatalf("expected mixed-source validation error\n%s", output)
	}
}

func TestParameterContextsValidationFailsForEnterpriseAuthWithoutInitialAdminIdentity(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "auth.mode=ldap",
		"--set", "auth.ldap.url=ldaps://ldap.example.com:636",
		"--set", "auth.ldap.managerSecret.name=nifi-ldap-bind",
		"--set", "auth.ldap.userSearch.base=ou=People,dc=example,dc=com",
		"--set", "auth.ldap.userSearch.filter=(uid={0})",
		"--set", "auth.ldap.groupSearch.base=ou=Groups,dc=example,dc=com",
		"--set", "auth.ldap.groupSearch.nameAttribute=cn",
		"--set", "auth.ldap.groupSearch.memberAttribute=member",
		"--set", "authz.mode=ldapSync",
		"--set", "authz.bootstrap.initialAdminGroup=nifi-platform-admins",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=runtime",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.baseUrl",
		"--set", "parameterContexts.contexts[0].parameters[0].value=https://api.internal.example.com",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when enterprise auth is enabled without an explicit initial admin identity\n%s", output)
	}
	if !strings.Contains(output, "parameterContexts.enabled=true with auth.mode=oidc or auth.mode=ldap requires authz.bootstrap.initialAdminIdentity") {
		t.Fatalf("expected auth-mode validation error\n%s", output)
	}
}

func TestPlatformManagedParameterContextsExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-parameter-contexts-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected parameterContexts example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-parameter-contexts",
		"platform-runtime",
		`"catalogMode": "runtime-managed"`,
		`"manualNiFiDrift": "reconciled-live-within-bounded-scope"`,
		`python3 /opt/nifi/fabric/parameter-contexts/bootstrap.py --once`,
		`mountPath: /opt/nifi/fabric/parameter-context-secrets`,
		`name: test-nifi-parameter-contexts`,
		`"secretRef": {`,
		`"name": "platform-parameter-context"`,
		`"name": "shared-secrets-provider"`,
		`"bootstrapMode": "referenced-only"`,
		`"rootProcessGroupName": "platform-target"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestParameterContextsValidationFailsForDuplicateAttachmentTarget(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=runtime-a",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.baseUrl",
		"--set", "parameterContexts.contexts[0].parameters[0].value=https://api-a.internal.example.com",
		"--set", "parameterContexts.contexts[0].attachments[0].rootProcessGroupName=payments",
		"--set", "parameterContexts.contexts[1].name=runtime-b",
		"--set", "parameterContexts.contexts[1].parameters[0].name=api.baseUrl",
		"--set", "parameterContexts.contexts[1].parameters[0].value=https://api-b.internal.example.com",
		"--set", "parameterContexts.contexts[1].attachments[0].rootProcessGroupName=payments",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for duplicate root child attachment target\n%s", output)
	}
	if !strings.Contains(output, `parameterContexts.contexts[1].attachments[0].rootProcessGroupName="payments" is already declared by another context`) {
		t.Fatalf("expected duplicate attachment validation error\n%s", output)
	}
}

func TestPlatformManagedParameterContextsKindExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-parameter-contexts-values.yaml",
		"-f", "examples/platform-managed-parameter-contexts-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected parameterContexts kind example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`image: "apache/nifi:2.8.0"`,
		`replicas: 1`,
		`name: test-nifi-parameter-contexts`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
