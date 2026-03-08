package controller

import "testing"

func TestNormalizeNodeAddressStripsSchemeAndPort(t *testing.T) {
	tests := map[string]string{
		"https://nifi-2.nifi-headless.nifi.svc.cluster.local:8443": "nifi-2.nifi-headless.nifi.svc.cluster.local",
		"http://10.244.0.10:8443":                                  "10.244.0.10",
		"nifi-2.nifi-headless.nifi.svc.cluster.local:8443":         "nifi-2.nifi-headless.nifi.svc.cluster.local",
		"10.244.0.10": "10.244.0.10",
	}

	for input, expected := range tests {
		if actual := normalizeNodeAddress(input); actual != expected {
			t.Fatalf("normalizeNodeAddress(%q) = %q, want %q", input, actual, expected)
		}
	}
}
