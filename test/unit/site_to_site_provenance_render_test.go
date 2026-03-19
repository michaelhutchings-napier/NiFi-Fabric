package unit

import (
	"strings"
	"testing"
)

func TestSiteToSiteProvenanceValidationFailsWithoutAuthorizedIdentityForSecureModes(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when secure Site-to-Site provenance auth has no authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization") {
		t.Fatalf("expected missing authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForHTTPSWithAuthNone(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=none",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for https siteToSiteProvenance auth.type=none\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.type=none cannot be used with an https:// destination.url") {
		t.Fatalf("expected https auth-type validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForAuthNoneWithAuthorizedIdentity(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=http://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=none",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when auth.type=none still sets authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.authorizedIdentity must be empty when auth.type=none") {
		t.Fatalf("expected auth none authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForHTTPWithSecureAuth(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=http://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for http siteToSiteProvenance auth.type=workloadTLS\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.type must be none for an http:// destination.url") {
		t.Fatalf("expected http secure auth-type validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForMissingAuthSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=secretRef",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for missing siteToSiteProvenance auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.secretRef.name is required when auth.type=secretRef") {
		t.Fatalf("expected missing siteToSiteProvenance auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForSecretRefContradictionWithWorkloadTLS(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.name=site-to-site-provenance-client",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when workloadTLS sets secretRef fields\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.secretRef.* cannot be set when auth.type=workloadTLS") {
		t.Fatalf("expected workloadTLS secretRef contradiction validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForSecretRefContradictionWithAuthNone(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=http://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=none",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.name=site-to-site-provenance-client",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when auth.type=none sets secretRef fields\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.secretRef.* cannot be set when auth.type=none") {
		t.Fatalf("expected auth none secretRef contradiction validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForIncompleteSecretRefMaterial(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=secretRef",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.name=site-to-site-provenance-client",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.keystoreKey=keystore.p12",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.keystorePasswordKey=keystore-password",
		"--set-string", "observability.siteToSiteProvenance.auth.secretRef.truststoreKey=",
		"--set", "observability.siteToSiteProvenance.auth.secretRef.truststorePasswordKey=truststore-password",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for incomplete siteToSiteProvenance auth Secret material\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.auth.secretRef.truststoreKey is required when auth.type=secretRef") {
		t.Fatalf("expected incomplete siteToSiteProvenance auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForInvalidTransportProtocol(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
		"--set", "observability.siteToSiteProvenance.transport.protocol=UDP",
		"--set", "observability.siteToSiteProvenance.transport.communicationsTimeout=30 secs",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid siteToSiteProvenance transport protocol\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.transport.protocol must be one of: RAW, HTTP") {
		t.Fatalf("expected invalid transport protocol validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsOutsideSingleUserAuth(t *testing.T) {
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
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail outside singleUser auth mode\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.enabled=true currently requires auth.mode=singleUser") {
		t.Fatalf("expected singleUser boundary validation error\n%s", output)
	}
}

func TestSiteToSiteProvenanceValidationFailsForInvalidStartPosition(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.siteToSiteProvenance.enabled=true",
		"--set", "observability.siteToSiteProvenance.destination.url=https://provenance-receiver.example.com/nifi",
		"--set", "observability.siteToSiteProvenance.destination.inputPortName=nifi-provenance",
		"--set", "observability.siteToSiteProvenance.auth.type=workloadTLS",
		"--set", "observability.siteToSiteProvenance.auth.authorizedIdentity=O=Example\\, CN=nifi-provenance-sender",
		"--set", "observability.siteToSiteProvenance.provenance.startPosition=oldestFirst",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid siteToSiteProvenance startPosition\n%s", output)
	}
	if !strings.Contains(output, "observability.siteToSiteProvenance.provenance.startPosition must be one of: beginningOfStream, endOfStream") {
		t.Fatalf("expected startPosition validation error\n%s", output)
	}
}

func TestPlatformManagedSiteToSiteProvenanceExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-site-to-site-provenance-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected siteToSiteProvenance example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-site-to-site-provenance",
		"fabric-site-to-site-provenance-export",
		"org.apache.nifi.reporting.SiteToSiteProvenanceReportingTask",
		"authorizedIdentity\": \"O=Example, CN=nifi-provenance-sender\"",
		"\"startPosition\": \"endOfStream\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedSiteToSiteProvenanceKindDeliveryExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-site-to-site-provenance-values.yaml",
		"-f", "examples/platform-managed-site-to-site-provenance-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected kind siteToSiteProvenance delivery example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"secretName: nifi-site-to-site-receiver-provenance-client",
		"https://site-to-site-receiver.site-to-site-receiver.svc.cluster.local:8443/nifi",
		"mountPath: /opt/nifi/fabric/site-to-site-provenance-ssl",
		"authorizedIdentity\": \"O=NiFi-Fabric, CN=nifi-site-to-site-provenance-client\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
