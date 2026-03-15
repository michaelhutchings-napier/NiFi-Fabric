package unit

import (
	"strings"
	"testing"
)

func TestSiteToSiteStatusValidationFailsWithoutAuthorizedIdentityForSecureModes(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=workloadTLS",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when secure Site-to-Site status auth has no authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization") {
		t.Fatalf("expected missing authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForHTTPSWithAuthNone(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=none",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for https siteToSiteStatus auth.type=none\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.type=none cannot be used with an https:// destination.url") {
		t.Fatalf("expected https auth-type validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForMissingAuthSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=secretRef",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for missing siteToSiteStatus auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.secretRef.name is required when auth.type=secretRef") {
		t.Fatalf("expected missing siteToSiteStatus auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsOutsideSingleUserAuth(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "auth.mode=oidc",
		"--set", "auth.oidc.discoveryUrl=https://issuer.example.com/realms/main/.well-known/openid-configuration",
		"--set", "auth.oidc.clientId=nifi",
		"--set", "auth.oidc.clientSecret.existingSecret=nifi-oidc",
		"--set", "auth.oidc.claims.identifyingUser=email",
		"--set", "auth.oidc.claims.groups=groups",
		"--set", "authz.bootstrap.initialAdminGroup=nifi-admins",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=workloadTLS",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail outside singleUser auth mode\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.enabled=true currently requires auth.mode=singleUser") {
		t.Fatalf("expected singleUser boundary validation error\n%s", output)
	}
}

func TestPlatformManagedSiteToSiteStatusExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-site-to-site-status-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected siteToSiteStatus example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-site-to-site-status",
		"fabric-site-to-site-status-export",
		"org.apache.nifi.reporting.SiteToSiteStatusReportingTask",
		"authorizedIdentity\": \"O=Example, CN=nifi-status-sender\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedSiteToSiteStatusKindDeliveryExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-site-to-site-status-values.yaml",
		"-f", "examples/platform-managed-site-to-site-status-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected kind siteToSiteStatus delivery example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"secretName: nifi-site-to-site-receiver-status-client",
		"https://site-to-site-receiver.site-to-site-receiver.svc.cluster.local:8443/nifi",
		"mountPath: /opt/nifi/fabric/site-to-site-status-ssl",
		"authorizedIdentity\": \"O=NiFi-Fabric, CN=nifi-site-to-site-status-client\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
