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

func TestSiteToSiteStatusValidationFailsForAuthNoneWithAuthorizedIdentity(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=http://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=none",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when auth.type=none still sets authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.authorizedIdentity must be empty when auth.type=none") {
		t.Fatalf("expected auth none authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForHTTPWithSecureAuth(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=http://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=workloadTLS",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for http siteToSiteStatus auth.type=workloadTLS\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.type must be none for an http:// destination.url") {
		t.Fatalf("expected http secure auth-type validation error\n%s", output)
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

func TestSiteToSiteStatusValidationFailsForSecretRefContradictionWithWorkloadTLS(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=workloadTLS",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
		"--set", "observability.siteToSiteStatus.auth.secretRef.name=site-to-site-status-client",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when workloadTLS sets secretRef fields\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.secretRef.* cannot be set when auth.type=workloadTLS") {
		t.Fatalf("expected workloadTLS secretRef contradiction validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForSecretRefContradictionWithAuthNone(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=http://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=none",
		"--set", "observability.siteToSiteStatus.auth.secretRef.name=site-to-site-status-client",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when auth.type=none sets secretRef fields\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.secretRef.* cannot be set when auth.type=none") {
		t.Fatalf("expected auth none secretRef contradiction validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForIncompleteSecretRefMaterial(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=secretRef",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
		"--set", "observability.siteToSiteStatus.auth.secretRef.name=site-to-site-status-client",
		"--set", "observability.siteToSiteStatus.auth.secretRef.keystoreKey=keystore.p12",
		"--set", "observability.siteToSiteStatus.auth.secretRef.keystorePasswordKey=keystore-password",
		"--set-string", "observability.siteToSiteStatus.auth.secretRef.truststoreKey=",
		"--set", "observability.siteToSiteStatus.auth.secretRef.truststorePasswordKey=truststore-password",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for incomplete siteToSiteStatus auth Secret material\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.auth.secretRef.truststoreKey is required when auth.type=secretRef") {
		t.Fatalf("expected incomplete siteToSiteStatus auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteStatusValidationFailsForInvalidTransportProtocol(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteStatus.enabled=true",
		"--set", "observability.siteToSiteStatus.destination.url=https://status-receiver.example.com/nifi",
		"--set", "observability.siteToSiteStatus.destination.inputPortName=nifi-status",
		"--set", "observability.siteToSiteStatus.auth.type=workloadTLS",
		"--set", "observability.siteToSiteStatus.auth.authorizedIdentity=O=Example\\, CN=nifi-status-sender",
		"--set", "observability.siteToSiteStatus.transport.protocol=UDP",
		"--set", "observability.siteToSiteStatus.transport.communicationsTimeout=30 secs",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid siteToSiteStatus transport protocol\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteStatus.transport.protocol must be one of: RAW, HTTP") {
		t.Fatalf("expected invalid transport protocol validation error\n%s", output)
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
