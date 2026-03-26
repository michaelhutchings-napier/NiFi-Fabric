package unit

import (
	"strings"
	"testing"
)

func TestIngressObjectHostRendersDeclaredHostname(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "ingress.enabled=true",
		"--set", "ingress.hosts[0].host=example.com",
		"--set", "ingress.hosts[0].paths[0].path=/",
		"--set", "ingress.hosts[0].paths[0].pathType=Prefix",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "kind: Ingress") {
		t.Fatalf("expected ingress to render\n%s", output)
	}
	if !strings.Contains(output, "host: example.com") {
		t.Fatalf("expected object-style ingress host to render the declared hostname\n%s", output)
	}
	if strings.Contains(output, "host: map[host:example.com") {
		t.Fatalf("expected object-style ingress host to avoid rendering the Go map representation\n%s", output)
	}
}

func TestIngressStringHostUsesDefaultRootPath(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "ingress.enabled=true",
		"--set", "ingress.hosts[0]=example.com",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"kind: Ingress",
		"host: example.com",
		"path: /",
		"pathType: Prefix",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}
