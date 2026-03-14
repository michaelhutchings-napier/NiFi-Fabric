package unit

import (
	"strings"
	"testing"
)

func TestOIDCGroupClaimsExampleRendersPolicyGroupsBeforeUsers(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	controllerReadPolicy := `<policy identifier="2b12e243-47bb-4c8c-82fb-8bce1cde872f" resource="/controller" action="R">`
	start := strings.Index(output, controllerReadPolicy)
	if start == -1 {
		t.Fatalf("expected rendered output to contain controller read policy\n%s", output)
	}
	end := strings.Index(output[start:], "</policy>")
	if end == -1 {
		t.Fatalf("expected rendered output to contain closing policy tag\n%s", output[start:])
	}
	policyBody := output[start : start+end]

	adminGroup := `<group identifier="9816433e-d325-e655-6542-191408365a81"/>`
	operatorGroup := `<group identifier="1163e273-26f6-87ef-bd86-1506e7c3e6c7"/>`
	nodeUser := `<user identifier="__NODE_IDENTITY_ID__"/>`

	adminGroupIndex := strings.Index(policyBody, adminGroup)
	operatorGroupIndex := strings.Index(policyBody, operatorGroup)
	nodeUserIndex := strings.Index(policyBody, nodeUser)
	if adminGroupIndex == -1 || operatorGroupIndex == -1 || nodeUserIndex == -1 {
		t.Fatalf("expected rendered policy to contain admin group, operator group, and node user bindings\n%s", policyBody)
	}
	if adminGroupIndex > nodeUserIndex || operatorGroupIndex > nodeUserIndex {
		t.Fatalf("expected policy group bindings to render before user bindings\n%s", policyBody)
	}
}

func TestBitbucketFlowRegistryKindExampleRendersPreparedClient(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/nifi-2.8.0-values.yaml",
		"-f", "examples/bitbucket-flow-registry-kind-values.yaml",
		"-f", "examples/test-fast-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`"name": "bitbucket-flows-kind"`,
		`"provider": "bitbucket"`,
		`"implementationClass": "org.apache.nifi.atlassian.bitbucket.BitbucketFlowRegistryClient"`,
		`"Form Factor": "CLOUD"`,
		`"Bitbucket API Instance": "http://bitbucket-mock.nifi.svc.cluster.local:8080"`,
		`"Repository Name": "nifi-flows"`,
		`"Workspace Name": "example-workspace"`,
		`"Web Client Service": "bitbucket-web-client"`,
		`"Authentication Type": "ACCESS_TOKEN"`,
		`"Parameter Context Values": "RETAIN"`,
		`"Access Token": {`,
		`"secretName": "bitbucket-flow-registry"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}
