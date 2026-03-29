package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesRepositoryEncryptionConfigAndMounts(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "repositoryEncryption.enabled=true",
		"--set", "repositoryEncryption.key.id=nifi-repository-key",
		"--set", "repositoryEncryption.secretRef.name=nifi-repository-encryption",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with repository encryption to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"nifi.repository.encryption.protocol.version=1",
		"nifi.repository.encryption.key.id=nifi-repository-key",
		"nifi.repository.encryption.key.provider=KEYSTORE",
		"nifi.repository.encryption.key.provider.keystore.location=/opt/nifi/repository-encryption/repository.p12",
		"nifi.repository.encryption.key.provider.keystore.password=__REPOSITORY_ENCRYPTION_KEYSTORE_PASSWORD__",
		"name: REPOSITORY_ENCRYPTION_KEYSTORE_PASSWORD",
		`name: "nifi-repository-encryption"`,
		`key: "password"`,
		"replace_property \"nifi.repository.encryption.key.provider.keystore.password\" \"${REPOSITORY_ENCRYPTION_KEYSTORE_PASSWORD}\"",
		"name: repository-encryption",
		"mountPath: /opt/nifi/repository-encryption",
		"secretName: nifi-repository-encryption",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderFailsWhenRepositoryEncryptionKeyIDMissing(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "repositoryEncryption.enabled=true",
		"--set", "repositoryEncryption.secretRef.name=nifi-repository-encryption",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when repositoryEncryption.key.id is missing\n%s", output)
	}
	if !strings.Contains(output, "repositoryEncryption.key.id is required when repositoryEncryption.enabled=true") {
		t.Fatalf("expected missing key id validation error\n%s", output)
	}
}

func TestNiFiRenderFailsWhenRepositoryEncryptionSecretNameMissing(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "repositoryEncryption.enabled=true",
		"--set", "repositoryEncryption.key.id=nifi-repository-key",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when repositoryEncryption.secretRef.name is missing\n%s", output)
	}
	if !strings.Contains(output, "repositoryEncryption.secretRef.name is required when repositoryEncryption.enabled=true") {
		t.Fatalf("expected missing secret name validation error\n%s", output)
	}
}

func TestNiFiRenderFailsForRepositoryEncryptionPropertyConflicts(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "repositoryEncryption.enabled=true",
		"--set", "repositoryEncryption.key.id=nifi-repository-key",
		"--set", "repositoryEncryption.secretRef.name=nifi-repository-encryption",
		"--set", "config.extraProperties.nifi\\.repository\\.encryption\\.key\\.id=other-key",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for repository encryption property conflicts\n%s", output)
	}
	if !strings.Contains(output, `config.extraProperties["nifi.repository.encryption.key.id"] conflicts with repositoryEncryption.*; use the explicit repositoryEncryption surface instead`) {
		t.Fatalf("expected property conflict validation error\n%s", output)
	}
}

func TestPlatformManagedRenderAddsRepositoryEncryptionSecretToRestartTriggers(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"--set", "nifi.repositoryEncryption.enabled=true",
		"--set", "nifi.repositoryEncryption.key.id=nifi-repository-key",
		"--set", "nifi.repositoryEncryption.secretRef.name=nifi-repository-encryption",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with repository encryption to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`- name: "nifi-repository-encryption"`,
		"secretName: nifi-repository-encryption",
		"name: REPOSITORY_ENCRYPTION_KEYSTORE_PASSWORD",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
