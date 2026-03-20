package unit

import (
	"strings"
	"testing"
)

func TestPlatformManagedQuickstartRendersGeneratedAuthAndTLSBootstrap(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-quickstart-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected quickstart managed render to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"name: nifi-auth",
		"app.kubernetes.io/component: quickstart",
		"name: test-quickstart-tls-bootstrap",
		"kind: Job",
		"kind: Role",
		"kind: RoleBinding",
		"kind: ServiceAccount",
		"name: generate-tls",
		"name: create-secret",
		"value: \"nifi-tls\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedCertManagerQuickstartRendersGeneratedParamsSecret(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-cert-manager-quickstart-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected cert-manager quickstart render to succeed: %v\n%s", err, output)
	}

	for _, want := range []string{
		"name: nifi-auth",
		"name: nifi-tls-params",
		"pkcs12Password:",
		"sensitivePropsKey:",
		"kind: Certificate",
		"secretName: nifi-tls",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}

	if strings.Contains(output, "test-quickstart-tls-bootstrap") {
		t.Fatalf("did not expect the external-secret TLS bootstrap job in cert-manager quickstart mode\n%s", output)
	}
}

func TestPlatformQuickstartRejectsOIDC(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-quickstart-values.yaml",
		"--set", "nifi.auth.mode=oidc",
		"--set", "nifi.auth.oidc.discoveryUrl=https://issuer.example/.well-known/openid-configuration",
		"--set", "nifi.auth.oidc.clientId=nifi",
		"--set", "nifi.auth.oidc.clientSecret.existingSecret=oidc-secret",
		"--set", "nifi.authz.mode=externalClaimGroups",
		"--set", "nifi.authz.bootstrap.initialAdminGroup=nifi-platform-admins",
	)
	if err == nil {
		t.Fatalf("expected quickstart to reject oidc auth mode\n%s", output)
	}
	if !strings.Contains(output, "quickstart.enabled=true requires nifi.auth.mode=singleUser") {
		t.Fatalf("expected quickstart oidc validation failure in output\n%s", output)
	}
}

func TestPlatformQuickstartRejectsLDAP(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-quickstart-values.yaml",
		"--set", "nifi.auth.mode=ldap",
		"--set", "nifi.auth.ldap.url=ldaps://ldap.example:636",
		"--set", "nifi.auth.ldap.managerSecret.name=ldap-bind",
		"--set", "nifi.auth.ldap.userSearch.base=ou=users,dc=example,dc=com",
		"--set", "nifi.auth.ldap.userSearch.filter=(uid={0})",
		"--set", "nifi.auth.ldap.groupSearch.base=ou=groups,dc=example,dc=com",
		"--set", "nifi.auth.ldap.groupSearch.nameAttribute=cn",
		"--set", "nifi.auth.ldap.groupSearch.memberAttribute=member",
		"--set", "nifi.authz.mode=ldapSync",
		"--set", "nifi.authz.bootstrap.initialAdminGroup=nifi-platform-admins",
	)
	if err == nil {
		t.Fatalf("expected quickstart to reject ldap auth mode\n%s", output)
	}
	if !strings.Contains(output, "quickstart.enabled=true requires nifi.auth.mode=singleUser") {
		t.Fatalf("expected quickstart ldap validation failure in output\n%s", output)
	}
}
