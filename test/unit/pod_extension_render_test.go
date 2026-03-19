package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesExtraInitContainersAndSidecars(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set-json", `extraInitContainers=[{"name":"extra-init","image":"busybox:1.36","command":["sh","-c","echo init"]}]`,
		"--set-json", `sidecars=[{"name":"extra-sidecar","image":"busybox:1.36","command":["sh","-c","sleep 3600"]}]`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with pod extensions to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: init-conf",
		"name: extra-init",
		"image: busybox:1.36",
		"runAsUser: 1000",
		"name: extra-sidecar",
		"runAsUser: 1000",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderAppliesContainerSecurityContextToBuiltInInitConf(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "securityContext.runAsUser=1234",
		"--set", "securityContext.runAsGroup=1234",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with init-conf security context to succeed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "name: init-conf") || !strings.Contains(output, "runAsUser: 1234") {
		t.Fatalf("expected built-in init-conf container to render the shared container security context\n%s", output)
	}
}

func TestNiFiRenderDefaultsPodHardeningSettings(t *testing.T) {
	output, err := helmTemplate(t, "charts/nifi")
	if err != nil {
		t.Fatalf("expected default nifi chart render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"automountServiceAccountToken: false",
		"enableServiceLinks: false",
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

func TestNiFiRenderAllowsPerContainerSecurityOverridesForPodExtensions(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "extraInitContainersSecurityContext.runAsUser=1500",
		"--set", "sidecarsSecurityContext.runAsUser=1600",
		"--set-json", `extraInitContainers=[{"name":"extra-init","image":"busybox:1.36","command":["sh","-c","echo init"],"securityContext":{"runAsUser":2500}}]`,
		"--set-json", `sidecars=[{"name":"extra-sidecar","image":"busybox:1.36","command":["sh","-c","sleep 3600"],"securityContext":{"runAsUser":2600}}]`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with pod extension security overrides to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: extra-init",
		"runAsUser: 2500",
		"name: extra-sidecar",
		"runAsUser: 2600",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedRenderIncludesNestedPodExtensions(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set-json", `nifi.extraInitContainers=[{"name":"extra-init","image":"busybox:1.36","command":["sh","-c","echo init"]}]`,
		"--set-json", `nifi.sidecars=[{"name":"extra-sidecar","image":"busybox:1.36","command":["sh","-c","sleep 3600"]}]`,
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested pod extensions to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: init-conf",
		"name: extra-init",
		"name: extra-sidecar",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderIncludesAdditionalPodShapeExtensions(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set-json", `imagePullSecrets=[{"name":"registry-creds"}]`,
		"--set", "automountServiceAccountToken=true",
		"--set", "enableServiceLinks=true",
		"--set-json", `podLabels={"team":"data-platform"}`,
		"--set-json", `hostAliases=[{"ip":"192.0.2.10","hostnames":["nifi-ext.local"]}]`,
		"--set", "priorityClassName=high-priority",
		"--set-json", `env=[{"name":"EXTRA_ENV","value":"enabled"}]`,
		"--set-json", `envFrom=[{"secretRef":{"name":"nifi-extra-env"}}]`,
		"--set-json", `extraVolumes=[{"name":"custom-bundle","configMap":{"name":"nifi-custom-bundle"}}]`,
		"--set-json", `extraVolumeMounts=[{"name":"custom-bundle","mountPath":"/opt/nifi/custom","readOnly":true}]`,
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with pod shape extensions to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: registry-creds",
		"automountServiceAccountToken: true",
		"enableServiceLinks: true",
		"team: data-platform",
		"priorityClassName: \"high-priority\"",
		"ip: 192.0.2.10",
		"- nifi-ext.local",
		"name: EXTRA_ENV",
		"value: enabled",
		"secretRef:",
		"name: nifi-extra-env",
		"name: custom-bundle",
		"mountPath: /opt/nifi/custom",
		"name: nifi-custom-bundle",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedRenderIncludesNestedPodShapeExtensions(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set-json", `nifi.imagePullSecrets=[{"name":"registry-creds"}]`,
		"--set", "nifi.automountServiceAccountToken=true",
		"--set", "nifi.enableServiceLinks=true",
		"--set-json", `nifi.podLabels={"team":"data-platform"}`,
		"--set-json", `nifi.hostAliases=[{"ip":"192.0.2.10","hostnames":["nifi-ext.local"]}]`,
		"--set", "nifi.priorityClassName=high-priority",
		"--set-json", `nifi.env=[{"name":"EXTRA_ENV","value":"enabled"}]`,
		"--set-json", `nifi.envFrom=[{"secretRef":{"name":"nifi-extra-env"}}]`,
		"--set-json", `nifi.extraVolumes=[{"name":"custom-bundle","configMap":{"name":"nifi-custom-bundle"}}]`,
		"--set-json", `nifi.extraVolumeMounts=[{"name":"custom-bundle","mountPath":"/opt/nifi/custom","readOnly":true}]`,
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested pod shape extensions to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: registry-creds",
		"automountServiceAccountToken: true",
		"enableServiceLinks: true",
		"team: data-platform",
		"priorityClassName: \"high-priority\"",
		"ip: 192.0.2.10",
		"name: EXTRA_ENV",
		"name: custom-bundle",
		"mountPath: /opt/nifi/custom",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
