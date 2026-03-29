package unit

import (
	"strings"
	"testing"
)

func TestNiFiRenderIncludesPerRepositoryStorageClassOverrides(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "persistence.storageClassName=shared-tier",
		"--set", "persistence.databaseRepository.storageClassName=fast-ssd",
		"--set", "persistence.contentRepository.storageClassName=capacity-tier",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render with per-repository storage classes to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: database-repository",
		"storageClassName: fast-ssd",
		"name: flowfile-repository",
		"storageClassName: shared-tier",
		"name: content-repository",
		"storageClassName: capacity-tier",
		"name: provenance-repository",
		"storageClassName: shared-tier",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRenderOmitsStorageClassNamesWhenNoFallbackOrOverridesAreSet(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "persistence.storageClassName=",
	)
	if err != nil {
		t.Fatalf("expected nifi chart render without explicit storage classes to succeed: %v\n%s", err, output)
	}
	if strings.Contains(output, "storageClassName:") {
		t.Fatalf("did not expect explicit storageClassName entries when no fallback or per-repository overrides are set\n%s", output)
	}
}

func TestPlatformManagedRenderIncludesNestedPerRepositoryStorageClassOverrides(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-storage-classes-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform chart render with nested storage classes to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: database-repository",
		"storageClassName: fast-ssd",
		"name: flowfile-repository",
		"storageClassName: fast-ssd",
		"name: content-repository",
		"storageClassName: capacity-tier",
		"name: provenance-repository",
		"storageClassName: capacity-tier",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
