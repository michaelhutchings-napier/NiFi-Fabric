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

func TestExporterMetricsCanDisableServiceMonitorWhileKeepingService(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.serviceMonitor.enabled=false",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "kind: ServiceMonitor") {
		t.Fatalf("expected exporter ServiceMonitor to be omitted when disabled\n%s", output)
	}
	for _, want := range []string{
		"kind: Deployment",
		"name: test-nifi-metrics-exporter",
		"kind: Service",
		"name: test-nifi-metrics",
		"targetPort: metrics",
		"app.kubernetes.io/component: metrics-exporter",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestExporterMetricsValidationFailsWhenServiceMonitorEnabledWithoutService(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.service.enabled=false",
		"--set", "observability.metrics.exporter.serviceMonitor.enabled=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when exporter ServiceMonitor is enabled without the Service\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.service.enabled=false cannot be combined with an enabled exporter ServiceMonitor") {
		t.Fatalf("expected exporter Service/ServiceMonitor validation error\n%s", output)
	}
}

func TestExporterMetricsRendersConfigMapCABundleConsumer(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.configMapRef.name=nifi-metrics-ca",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.configMapRef.key=ca.crt",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: exporter-ca",
		"configMap:",
		"name: nifi-metrics-ca",
		"mountPath: /var/run/nifi-metrics-ca",
		"name: EXPORTER_TLS_CA_FILE",
		"/var/run/nifi-metrics-ca/ca.crt",
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

func TestPlatformManagedTrustManagerExporterExampleRendersSecretTarget(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-trust-manager-values.yaml",
		"-f", "examples/platform-managed-metrics-exporter-values.yaml",
		"-f", "examples/platform-managed-metrics-exporter-trust-manager-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Bundle",
		"secret:",
		"name: test-nifi-trust-bundle",
		"kind: Deployment",
		"name: test-nifi-metrics-exporter",
		"configMap:",
		"name: test-nifi-metrics-exporter-config",
		"secretName: test-nifi-trust-bundle",
		"EXPORTER_TLS_CA_FILE",
		"/var/run/nifi-metrics-ca/ca.crt",
		"kind: ServiceMonitor",
		"name: test-nifi-exporter",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestExporterMetricsTrustManagerBundleRendersSecretCA(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "trustManagerBundleRef.type=secret",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle=true",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Deployment",
		"name: test-nifi-metrics-exporter",
		"name: exporter-ca",
		"secretName: test-nifi-trust-bundle",
		"EXPORTER_TLS_CA_FILE",
		"/var/run/nifi-metrics-ca/ca.crt",
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

func TestExporterMetricsValidationFailsForConflictingTrustManagerCAInputs(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=exporter",
		"--set", "observability.metrics.exporter.machineAuth.secretRef.name=nifi-metrics-auth",
		"--set", "observability.metrics.exporter.machineAuth.authorization.type=Bearer",
		"--set", "observability.metrics.exporter.machineAuth.authorization.credentialsKey=token",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle=true",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.secretRef.name=nifi-metrics-ca",
		"--set", "observability.metrics.exporter.source.tlsConfig.ca.secretRef.key=ca.crt",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for conflicting exporter CA inputs\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle cannot be combined") {
		t.Fatalf("expected conflicting exporter CA validation error\n%s", output)
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

func TestPlatformTrustManagerValidationFailsForExporterBundleRefMismatch(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-trust-manager-values.yaml",
		"-f", "examples/platform-managed-metrics-exporter-values.yaml",
		"--set", "trustManager.target.type=secret",
		"--set", "nifi.observability.metrics.exporter.source.tlsConfig.ca.secretRef.name=",
		"--set", "nifi.observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for mismatched exporter trust-manager bundle ref type\n%s", output)
	}
	if !strings.Contains(output, "nifi.trustManagerBundleRef.type must match trustManager.target.type") {
		t.Fatalf("expected exporter platform bundle ref mismatch validation error\n%s", output)
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

func TestSiteToSiteMetricsRendersTypedBootstrap(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
	)
	if err != nil {
		t.Fatalf("expected helm template to succeed for typed siteToSite mode: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-site-to-site-metrics",
		"app.kubernetes.io/component: metrics-site-to-site",
		"python3 /opt/nifi/fabric/site-to-site-metrics/bootstrap.py",
		"mountPath: /opt/nifi/fabric/site-to-site-metrics",
		"fabric-site-to-site-metrics-export",
		"org.apache.nifi.reporting.SiteToSiteMetricsReportingTask",
		"org.apache.nifi.ssl.StandardRestrictedSSLContextService",
		"authorizedIdentity\": \"O=Example, CN=nifi-metrics-sender\"",
		"destination-input-port",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestSiteToSiteMetricsRendersCustomTLSSecretMount(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=secretRef",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-secretref-sender",
		"--set", "observability.metrics.siteToSite.auth.secretRef.name=nifi-site-to-site-tls",
	)
	if err != nil {
		t.Fatalf("expected helm template to succeed for siteToSite secretRef auth: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: site-to-site-metrics-ssl",
		"secretName: nifi-site-to-site-tls",
		"name: SITE_TO_SITE_METRICS_KEYSTORE_PASSWORD",
		"name: SITE_TO_SITE_METRICS_TRUSTSTORE_PASSWORD",
		"mountPath: /opt/nifi/fabric/site-to-site-metrics-ssl",
		"secretName\": \"nifi-site-to-site-tls\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestSiteToSiteValidationFailsWithoutDestinationURL(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail without a siteToSite destination url\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.destination.url is required") {
		t.Fatalf("expected missing destination url validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsWhenDisabledFlagIsMissing(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.destination.url=http://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when the siteToSite enabled flag is missing\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.enabled=true is required") {
		t.Fatalf("expected missing enabled-flag validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForUnsupportedTransportProtocol(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
		"--set", "observability.metrics.siteToSite.transport.protocol=TCP",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for an unsupported siteToSite transport protocol\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.transport.protocol must be one of: RAW, HTTP") {
		t.Fatalf("expected invalid siteToSite transport protocol validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForHTTPSWithoutTLSAuth(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=none",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for https siteToSite auth.type=none\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.type=none cannot be used with an https:// destination.url") {
		t.Fatalf("expected https auth-type validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsWithoutAuthorizedIdentityForSecureModes(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when secure Site-to-Site auth has no authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization") {
		t.Fatalf("expected missing authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsWhenAuthorizedIdentityIsSetForAuthNone(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=http://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=none",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when auth.type=none still sets an authorized identity\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.authorizedIdentity must be empty when auth.type=none") {
		t.Fatalf("expected auth.none authorized identity validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForHTTPWithTLSAuth(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=http://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for http siteToSite auth.type=workloadTLS\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.type must be none for an http:// destination.url") {
		t.Fatalf("expected http auth-type validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForMissingAuthSecretRef(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=secretRef",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-secretref-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for missing siteToSite auth Secret reference\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.secretRef.name is required when auth.type=secretRef") {
		t.Fatalf("expected missing siteToSite auth Secret validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForBlankSecretRefMaterialKeys(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=secretRef",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-secretref-sender",
		"--set", "observability.metrics.siteToSite.auth.secretRef.name=nifi-site-to-site-tls",
		"--set", "observability.metrics.siteToSite.auth.secretRef.truststoreKey=",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for blank siteToSite truststore key\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.auth.secretRef.truststoreKey is required when auth.type=secretRef") {
		t.Fatalf("expected missing siteToSite truststore key validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsOutsideSingleUserAuth(t *testing.T) {
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
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail outside singleUser auth mode\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.mode=siteToSite currently requires auth.mode=singleUser") {
		t.Fatalf("expected singleUser boundary validation error\n%s", output)
	}
}

func TestSiteToSiteValidationFailsForUnsupportedFormatType(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "observability.metrics.mode=siteToSite",
		"--set", "observability.metrics.siteToSite.enabled=true",
		"--set", "observability.metrics.siteToSite.destination.url=https://metrics-receiver.example.com/nifi",
		"--set", "observability.metrics.siteToSite.destination.inputPortName=nifi-metrics",
		"--set", "observability.metrics.siteToSite.auth.type=workloadTLS",
		"--set", "observability.metrics.siteToSite.auth.authorizedIdentity=O=Example\\, CN=nifi-metrics-sender",
		"--set", "observability.metrics.siteToSite.format.type=Record",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported siteToSite format type\n%s", output)
	}
	if !strings.Contains(output, "observability.metrics.siteToSite.format.type must be AmbariFormat for the typed site-to-site metrics feature") {
		t.Fatalf("expected unsupported siteToSite format validation error\n%s", output)
	}
}

func TestPlatformManagedSiteToSiteMetricsExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-metrics-site-to-site-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected siteToSite example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-site-to-site-metrics",
		"fabric-site-to-site-metrics-export",
		"org.apache.nifi.reporting.SiteToSiteMetricsReportingTask",
		"authorizedIdentity\": \"O=Example, CN=nifi-metrics-sender\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedSiteToSiteKindDeliveryExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-metrics-site-to-site-values.yaml",
		"-f", "examples/platform-managed-metrics-site-to-site-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected kind siteToSite delivery example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"secretName: nifi-site-to-site-receiver-client",
		"https://site-to-site-receiver.site-to-site-receiver.svc.cluster.local:8443/nifi",
		"mountPath: /opt/nifi/fabric/site-to-site-metrics-ssl",
		"authorizedIdentity\": \"O=NiFi-Fabric, CN=nifi-site-to-site-metrics-client\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestStandaloneSiteToSiteReceiverHarnessExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/standalone-site-to-site-receiver-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected standalone site-to-site receiver harness render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: site-to-site-receiver",
		"name: \"site-to-site-receiver-auth\"",
		"name: \"site-to-site-receiver-tls\"",
		"secretName: site-to-site-receiver-tls",
		"platform.nifi.io/controller-managed: \"false\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered receiver harness output to contain %q\n%s", want, output)
		}
	}
}
