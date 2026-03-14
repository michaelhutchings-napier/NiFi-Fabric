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
		"app.kubernetes.io/component: metrics",
		"port: 8443",
		"targetPort: https",
		"name: nifi-metrics-auth",
		"name: nifi-metrics-ca",
		"type: Bearer",
		"serverName: test-nifi.default.svc.cluster.local",
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

func TestNativeMetricsValidationFailsForDuplicateSanitizedEndpointNames(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=authorizationHeader",
		"--set", "observability.metrics.nativeApi.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.nativeApi.endpoints[0].name=flow",
		"--set", "observability.metrics.nativeApi.endpoints[0].enabled=true",
		"--set", "observability.metrics.nativeApi.endpoints[0].path=/nifi-api/flow/metrics/prometheus",
		"--set", "observability.metrics.nativeApi.endpoints[1].name=Flow",
		"--set", "observability.metrics.nativeApi.endpoints[1].enabled=true",
		"--set", "observability.metrics.nativeApi.endpoints[1].path=/nifi-api/flow/metrics/prometheus",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for duplicate endpoint names\n%s", output)
	}
	if !strings.Contains(output, "collides with another endpoint after Kubernetes name sanitizing") {
		t.Fatalf("expected duplicate sanitized endpoint-name validation error\n%s", output)
	}
}

func TestNativeMetricsValidationFailsForInvalidEndpointScheme(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=authorizationHeader",
		"--set", "observability.metrics.nativeApi.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.nativeApi.endpoints[0].name=flow",
		"--set", "observability.metrics.nativeApi.endpoints[0].enabled=true",
		"--set", "observability.metrics.nativeApi.endpoints[0].path=/nifi-api/flow/metrics/prometheus",
		"--set", "observability.metrics.nativeApi.endpoints[0].scheme=ftp",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for an invalid endpoint scheme\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.nativeApi.endpoints[flow].scheme must be one of: http, https") {
		t.Fatalf("expected invalid endpoint scheme validation error\n%s", output)
	}
}

func TestPlatformManagedExporterMetricsExampleRendersExporterResources(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-metrics-exporter-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Deployment",
		"name: test-nifi-metrics-exporter",
		"kind: Service",
		"name: test-nifi-metrics",
		"kind: ServiceMonitor",
		"name: test-nifi-exporter",
		"path: /metrics",
		"EXPORTER_SOURCE_PATH",
		"/nifi-api/flow/metrics/prometheus",
		"EXPORTER_FLOW_STATUS_ENABLED",
		"value: \"true\"",
		"EXPORTER_FLOW_STATUS_PATH",
		"/nifi-api/flow/status",
		"secretName: nifi-metrics-auth",
		"secretName: nifi-metrics-ca",
		"path: /readyz",
		"path: /healthz",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedTrustManagerExampleRendersBundleAndConsumers(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-trust-manager-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"apiVersion: trust.cert-manager.io/v1alpha1",
		"kind: Bundle",
		"name: test-nifi-trust-bundle",
		"app.kubernetes.io/component: trust",
		"kubernetes.io/metadata.name: default",
		"kind: CronJob",
		"name: test-nifi-trust-source-mirror",
		"name: test-nifi-tls-ca-source",
		"key: ca.crt",
		"name: additional-trust-bundle",
		"mountPath: /opt/nifi/trust-bundle",
		"TRUSTSTORE_PATH=\"/opt/nifi/tls/truststore.p12\"",
		"EXTRA_TRUST_BUNDLE_FILE=\"/opt/nifi/trust-bundle/ca.crt\"",
		"/opt/nifi/nifi-current/conf/truststore-with-extra-cas.p12",
		"- name: \"test-nifi-trust-bundle\"",
		"name: test-nifi-trust-bundle",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNativeMetricsTrustManagerBundleRendersConfigMapCA(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=authorizationHeader",
		"--set", "observability.metrics.nativeApi.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle=true",
		"--set", "observability.metrics.nativeApi.endpoints[0].name=flow",
		"--set", "observability.metrics.nativeApi.endpoints[0].enabled=true",
		"--set", "observability.metrics.nativeApi.endpoints[0].path=/nifi-api/flow/metrics/prometheus",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: ServiceMonitor",
		"name: test-nifi-flow",
		"tlsConfig:",
		"configMap:",
		"name: test-nifi-trust-bundle",
		"key: ca.crt",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedTrustManagerNativeMetricsExampleRendersSecretTarget(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-trust-manager-values.yaml",
		"-f", "examples/platform-managed-metrics-native-values.yaml",
		"-f", "examples/platform-managed-metrics-native-trust-manager-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Bundle",
		"secret:",
		"key: ca.crt",
		"pkcs12:",
		"key: truststore.p12",
		"profile: Modern2023",
		"kind: ServiceMonitor",
		"name: test-nifi-flow",
		"tlsConfig:",
		"secret:",
		"name: test-nifi-trust-bundle",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestExporterMetricsValidationFailsForInvalidServiceMonitorScheme(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.serviceMonitor.defaults.scheme=ftp",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for an invalid exporter ServiceMonitor scheme\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.serviceMonitor.defaults.scheme must be one of: http, https") {
		t.Fatalf("expected invalid exporter ServiceMonitor scheme validation error\n%s", output)
	}
}

func TestNativeMetricsValidationFailsForConflictingTrustManagerCAInputs(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=nativeApi",
		"--set", "observability.metrics.nativeApi.machineAuth.type=authorizationHeader",
		"--set", "observability.metrics.nativeApi.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.nativeApi.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle=true",
		"--set", "observability.metrics.nativeApi.tlsConfig.ca.secretRef.name=nifi-metrics-ca",
		"--set", "observability.metrics.nativeApi.tlsConfig.ca.secretRef.key=ca.crt",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for conflicting nativeApi CA inputs\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle cannot be combined") {
		t.Fatalf("expected conflicting nativeApi CA validation error\n%s", output)
	}
}

func TestAdditionalTrustBundleValidationFailsWithoutSource(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "tls.additionalTrustBundle.enabled=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without an additional trust bundle source\n%s", output)
	}
	if !strings.Contains(output, "tls.additionalTrustBundle requires useTrustManagerBundle=true or a configMapRef/secretRef") {
		t.Fatalf("expected additional trust bundle validation error\n%s", output)
	}
}

func TestPlatformTrustManagerValidationFailsWithoutSources(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set", "trustManager.enabled=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without trust-manager sources\n%s", output)
	}
	if !strings.Contains(output, "trustManager.enabled=true requires at least one source") {
		t.Fatalf("expected trust-manager source validation error\n%s", output)
	}
}

func TestPlatformTrustManagerValidationFailsForBundleRefMismatch(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set", "trustManager.enabled=true",
		"--set", "trustManager.sources.inline[0].pem=-----BEGIN CERTIFICATE-----\\nMIIB\\n-----END CERTIFICATE-----",
		"--set", "trustManager.target.type=secret",
		"--set", "nifi.tls.additionalTrustBundle.enabled=true",
		"--set", "nifi.tls.additionalTrustBundle.useTrustManagerBundle=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for mismatched trust-manager bundle ref type\n%s", output)
	}
	if !strings.Contains(output, "nifi.trustManagerBundleRef.type must match trustManager.target.type") {
		t.Fatalf("expected platform bundle ref mismatch validation error\n%s", output)
	}
}

func TestExporterMetricsValidationFailsForNonPositiveTimeout(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.source.timeoutSeconds=0",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for a non-positive exporter timeout\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.source.timeoutSeconds must be greater than zero") {
		t.Fatalf("expected non-positive exporter timeout validation error\n%s", output)
	}
}

func TestExporterMetricsValidationFailsWithoutMachineAuthSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without an exporter machine-auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.machineAuth.secretRef.name is required") {
		t.Fatalf("expected validation error for missing exporter machine-auth Secret reference\n%s", output)
	}
}

func TestExporterFlowStatusValidationFailsWithoutPath(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.supplemental.flowStatus.enabled=true",
		"--set", "observability.metrics.exporter.supplemental.flowStatus.path=",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without a flow-status path\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.supplemental.flowStatus.path is required") {
		t.Fatalf("expected validation error for missing flow-status path\n%s", output)
	}
}

func TestUnsupportedMetricsModeFailsClearly(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for siteToSite mode\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.mode=siteToSite remains prepared-only") {
		t.Fatalf("expected siteToSite-mode validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsWithoutDestinationURL(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without a siteToSite destination url\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.destination.url is required") {
		t.Fatalf("expected missing destination url validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForHTTPTLSContradiction(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=http://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.destination.tls.ca.secretRef.name=nifi-metrics-destination-ca",
		"--set", "observability.metrics.siteToSite.destination.tls.ca.secretRef.key=ca.crt",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for an http siteToSite destination with TLS config\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.destination.tls.* cannot be set for an http:// destination.url") {
		t.Fatalf("expected http/tls contradiction validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForUnsupportedTransportProtocol(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.transport.protocol=TCP",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for an unsupported siteToSite transport protocol\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.transport.protocol must be one of: RAW, HTTP") {
		t.Fatalf("expected invalid siteToSite transport protocol validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForMissingAuthSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.destination.auth.type=authorizationHeader",
		"--set", "observability.metrics.siteToSite.destination.auth.authorization.type=Bearer",
		"--set", "observability.metrics.siteToSite.destination.auth.authorization.credentialsKey=token",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for missing siteToSite auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.destination.auth.secretRef.name is required when destination auth is enabled") {
		t.Fatalf("expected missing siteToSite auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForUnsupportedFormatType(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.format.type=Record",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported siteToSite format type\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.format.type must be AmbariFormat in the current prepared contract") {
		t.Fatalf("expected unsupported siteToSite format validation error\n%s", output)
	}
}

func TestPlatformManagedSiteToSiteMetricsExampleFailsClearlyAsPreparedOnly(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-metrics-site-to-site-values.yaml",
	)
	if err == nil {
		t.Fatalf("expected siteToSite example render to fail until runtime wiring exists\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.mode=siteToSite remains prepared-only") {
		t.Fatalf("expected prepared-only siteToSite validation error\n%s", output)
	}
}
