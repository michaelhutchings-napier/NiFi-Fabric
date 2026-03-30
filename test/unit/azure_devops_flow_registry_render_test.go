package unit

import (
	"strings"
	"testing"
)

func TestAzureDevOpsFlowRegistryExampleRendersPreparedClientCatalog(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/standalone/values.yaml",
		"-f", "examples/azure-devops-flow-registry-values.yaml",
		"--set", "replicaCount=1",
	)
	if err != nil {
		t.Fatalf("expected azure devops flow registry example to render successfully: %v\n%s", err, output)
	}

	for _, want := range []string{
		`"provider": "azureDevOps"`,
		`"implementationClass": "org.apache.nifi.azure.devops.AzureDevOpsFlowRegistryClient"`,
		`"API URL": "https://dev.azure.com"`,
		`"Organization": "example-org"`,
		`"Project": "data-platform"`,
		`"Repository Name": "nifi-flows"`,
		`"OAuth2 Access Token Provider": "azure-devops-oauth2-provider"`,
		`"Web Client Service": "azure-devops-web-client"`,
		`"bootstrapMode": "prepared-only"`,
		`"controllerServiceName": "azure-devops-oauth2-provider"`,
		"name: test-nifi-flow-registry-clients",
		"mountPath: /opt/nifi/fabric/flow-registry-clients",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestAzureDevOpsFlowRegistryRenderFailsWithoutProject(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "flowRegistryClients.enabled=true",
		"--set", "flowRegistryClients.clients[0].name=azure-devops-flows",
		"--set", "flowRegistryClients.clients[0].provider=azureDevOps",
		"--set", "flowRegistryClients.clients[0].azureDevOps.apiUrl=https://dev.azure.com",
		"--set", "flowRegistryClients.clients[0].azureDevOps.organization=example-org",
		"--set", "flowRegistryClients.clients[0].repository.name=nifi-flows",
		"--set", "flowRegistryClients.clients[0].azureDevOps.webClientServiceName=azure-devops-web-client",
		"--set", "flowRegistryClients.clients[0].azureDevOps.oauth2AccessTokenProviderName=azure-devops-oauth2-provider",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when azureDevOps.project is missing\n%s", output)
	}
	if !strings.Contains(output, "flowRegistryClients.clients[0].azureDevOps.project is required for provider=azureDevOps") {
		t.Fatalf("expected missing azure devops project validation error\n%s", output)
	}
}
