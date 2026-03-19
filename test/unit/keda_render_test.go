package unit

import (
	"strings"
	"testing"
)

func TestPlatformManagedKEDAExampleRendersScaledObjectAgainstNiFiCluster(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-keda-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected KEDA example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"apiVersion: keda.sh/v1alpha1",
		"kind: ScaledObject",
		"apiVersion: platform.nifi.io/v1alpha1",
		"kind: NiFiCluster",
		"requestedReplicas: 0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedKEDAValidationFailsForDeclarativeRequestedReplicas(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-keda-values.yaml",
		"--set", "cluster.autoscaling.external.requestedReplicas=2",
	)
	if err == nil {
		t.Fatalf("expected KEDA render to fail when external requestedReplicas is hand-authored\n%s", output)
	}
	if !strings.Contains(output, "keda.enabled=true requires cluster.autoscaling.external.requestedReplicas=0 because the NiFiCluster /scale field is runtime-managed by KEDA and the controller") {
		t.Fatalf("expected runtime-managed /scale validation error\n%s", output)
	}
}

func TestPlatformManagedKEDAValidationFailsWhenKEDABoundsExceedControllerBounds(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-keda-values.yaml",
		"--set", "keda.maxReplicaCount=6",
	)
	if err == nil {
		t.Fatalf("expected KEDA render to fail when KEDA max exceeds controller max\n%s", output)
	}
	if !strings.Contains(output, "keda.maxReplicaCount must be less than or equal to cluster.autoscaling.maxReplicas so KEDA intent stays within the controller-owned autoscaling ceiling") {
		t.Fatalf("expected KEDA bounds validation error\n%s", output)
	}
}

func TestPlatformManagedKEDAValidationFailsWhenKEDAMinFallsBelowControllerFloor(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-keda-values.yaml",
		"--set", "keda.minReplicaCount=1",
	)
	if err == nil {
		t.Fatalf("expected KEDA render to fail when KEDA min falls below controller min\n%s", output)
	}
	if !strings.Contains(output, "keda.minReplicaCount must be greater than or equal to cluster.autoscaling.minReplicas so KEDA intent stays within the controller-owned autoscaling floor") {
		t.Fatalf("expected KEDA minimum-bounds validation error\n%s", output)
	}
}

func TestPlatformManagedKEDADownscaleValidationFailsWhenKEDAMinDiffersFromControllerFloor(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-keda-values.yaml",
		"-f", "examples/platform-managed-keda-scale-down-values.yaml",
		"--set", "keda.minReplicaCount=3",
	)
	if err == nil {
		t.Fatalf("expected KEDA render to fail when downscale min differs from the controller floor\n%s", output)
	}
	if !strings.Contains(output, "keda.minReplicaCount must equal cluster.autoscaling.minReplicas when cluster.autoscaling.external.scaleDownEnabled=true so KEDA's inactive floor matches the controller-owned safe downscale floor") {
		t.Fatalf("expected KEDA downscale floor alignment validation error\n%s", output)
	}
}
