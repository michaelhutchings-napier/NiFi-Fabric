package unit

import (
	"strings"
	"testing"
)

func preparedGitHubFlowRegistryClientArgs() []string {
	return []string{
		"--set", "flowRegistryClients.enabled=true",
		"--set", "flowRegistryClients.clients[0].name=github-flows",
		"--set", "flowRegistryClients.clients[0].provider=github",
		"--set", "flowRegistryClients.clients[0].repository.owner=example-org",
		"--set", "flowRegistryClients.clients[0].repository.name=nifi-flows",
		"--set", "flowRegistryClients.clients[0].github.auth.personalAccessTokenSecret.name=github-flow-registry",
		"--set", "flowRegistryClients.clients[0].github.auth.personalAccessTokenSecret.key=token",
	}
}

func preparedNiFiRegistryFlowRegistryClientArgs() []string {
	return []string{
		"--set", "flowRegistryClients.enabled=true",
		"--set", "flowRegistryClients.clients[0].name=nifi-registry-flows",
		"--set", "flowRegistryClients.clients[0].provider=nifiRegistry",
		"--set", "flowRegistryClients.clients[0].nifiRegistry.url=http://nifi-registry.nifi.svc.cluster.local:18080",
	}
}

func TestVersionedFlowImportsValidationFailsWithoutImports(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when versionedFlowImports is enabled without any imports\n%s", output)
	}
	if !strings.Contains(output, "versionedFlowImports.enabled=true requires versionedFlowImports.imports to contain at least one import definition") {
		t.Fatalf("expected missing imports validation error\n%s", output)
	}
}

func TestVersionedFlowImportsAllowsControllerBridgeWithoutInlineImports(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "controllerManaged.enabled=true",
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.controllerBridge.enabled=true",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err != nil {
		t.Fatalf("expected helm template to allow controller bridge without inline imports: %v\n%s", err, output)
	}
	for _, want := range []string{
		`"externalImportsPath": "/opt/nifi/fabric/nifidataflows/imports.json"`,
		`"statusConfigMapName": "test-nifi-nifidataflows-status"`,
		`name: nifidataflow-bridge`,
		`name: test-nifi-nifidataflows`,
		`name: test-nifi-nifidataflows-status`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestVersionedFlowImportsControllerBridgeRequiresManagedWorkload(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.controllerBridge.enabled=true",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when controller bridge is enabled without controllerManaged workload\n%s", output)
	}
	if !strings.Contains(output, "versionedFlowImports.controllerBridge.enabled=true requires controllerManaged.enabled=true") {
		t.Fatalf("expected controller bridge validation error\n%s", output)
	}
}

func TestVersionedFlowImportsValidationFailsForVersionWithWhitespace(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=release candidate",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for invalid versionedFlowImports version\n%s", output)
	}
	if !strings.Contains(output, "versionedFlowImports.imports[0].version must be \"latest\" or a non-empty version identifier without whitespace") {
		t.Fatalf("expected version validation error\n%s", output)
	}
}

func TestVersionedFlowImportsAllowsExplicitVersionIdentifier(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=0000000000000000000000000000000000000003",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err != nil {
		t.Fatalf("expected helm template to allow explicit version identifier: %v\n%s", err, output)
	}
	if !strings.Contains(output, `"version": "0000000000000000000000000000000000000003"`) {
		t.Fatalf("expected rendered output to contain explicit version identifier\n%s", output)
	}
}

func TestVersionedFlowImportsValidationFailsForUnknownPreparedParameterContextRef(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=payments-runtime",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.baseUrl",
		"--set", "parameterContexts.contexts[0].parameters[0].value=https://payments.internal.example.com",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
		"--set", "versionedFlowImports.imports[0].parameterContextRefs[0].name=missing-context",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unknown prepared parameter context reference\n%s", output)
	}
	if !strings.Contains(output, `versionedFlowImports.imports[0].parameterContextRefs[0].name="missing-context" is not present in parameterContexts.contexts[].name`) {
		t.Fatalf("expected parameter context reference validation error\n%s", output)
	}
}

func TestVersionedFlowImportsValidationFailsForOIDCWithoutInitialAdminIdentity(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=oidc",
		"--set", "auth.oidc.discoveryUrl=https://idp.example.com/.well-known/openid-configuration",
		"--set", "auth.oidc.clientId=nifi-fabric",
		"--set", "auth.oidc.clientSecret.existingSecret=nifi-oidc",
		"--set", "auth.oidc.claims.identifyingUser=email",
		"--set", "auth.oidc.claims.groups=groups",
		"--set", "authz.mode=externalClaimGroups",
		"--set", "authz.applicationGroups[0]=nifi-platform-admins",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for oidc versionedFlowImports without an explicit proxied admin identity\n%s", output)
	}
	if !strings.Contains(output, "versionedFlowImports.enabled=true with auth.mode=oidc or auth.mode=ldap requires authz.bootstrap.initialAdminIdentity so the bounded trusted-proxy management identity is explicit") {
		t.Fatalf("expected enterprise-auth validation error\n%s", output)
	}
}

func TestVersionedFlowImportsAllowsOIDCWithExplicitInitialAdminIdentity(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=oidc",
		"--set", "auth.oidc.discoveryUrl=https://idp.example.com/.well-known/openid-configuration",
		"--set", "auth.oidc.clientId=nifi-fabric",
		"--set", "auth.oidc.clientSecret.existingSecret=nifi-oidc",
		"--set", "auth.oidc.claims.identifyingUser=email",
		"--set", "auth.oidc.claims.groups=groups",
		"--set", "authz.mode=externalClaimGroups",
		"--set", "authz.applicationGroups[0]=nifi-platform-admins",
		"--set", "authz.bootstrap.initialAdminIdentity=alice@example.com",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err != nil {
		t.Fatalf("expected helm template to allow oidc versionedFlowImports with explicit proxied admin identity: %v\n%s", err, output)
	}
	for _, want := range []string{
		`"authMode": "oidc"`,
		`"proxiedIdentity": "alice@example.com"`,
		`"latestVersionPolicy": "resolve-on-create-or-declared-change-then-pin"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestVersionedFlowImportsAllowsLDAPWithExplicitInitialAdminIdentity(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=ldap",
		"--set", "auth.ldap.url=ldaps://ldap.example.com:636",
		"--set", "auth.ldap.managerSecret.name=nifi-ldap-bind",
		"--set", "auth.ldap.userSearch.base=ou=People,dc=example,dc=com",
		"--set", "auth.ldap.userSearch.filter=(uid={0})",
		"--set", "auth.ldap.groupSearch.base=ou=Groups,dc=example,dc=com",
		"--set", "auth.ldap.groupSearch.nameAttribute=cn",
		"--set", "auth.ldap.groupSearch.memberAttribute=member",
		"--set", "authz.mode=ldapSync",
		"--set", "authz.bootstrap.initialAdminIdentity=alice",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err != nil {
		t.Fatalf("expected helm template to allow ldap versionedFlowImports with explicit proxied admin identity: %v\n%s", err, output)
	}
	for _, want := range []string{
		`"authMode": "ldap"`,
		`"proxiedIdentity": "alice"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestVersionedFlowImportsValidationFailsForMultipleDirectParameterContextRefs(t *testing.T) {
	args := append(
		preparedGitHubFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "parameterContexts.enabled=true",
		"--set", "parameterContexts.contexts[0].name=payments-runtime",
		"--set", "parameterContexts.contexts[0].parameters[0].name=api.baseUrl",
		"--set", "parameterContexts.contexts[0].parameters[0].value=https://payments.internal.example.com",
		"--set", "parameterContexts.contexts[1].name=payments-shared",
		"--set", "parameterContexts.contexts[1].parameters[0].name=shared.region",
		"--set", "parameterContexts.contexts[1].parameters[0].value=eu-west-1",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
		"--set", "versionedFlowImports.imports[0].parameterContextRefs[0].name=payments-runtime",
		"--set", "versionedFlowImports.imports[0].parameterContextRefs[1].name=payments-shared",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for multiple direct parameterContextRefs\n%s", output)
	}
	if !strings.Contains(output, "versionedFlowImports.imports[0] supports at most one direct parameterContextRef in this slice") {
		t.Fatalf("expected direct parameter context reference validation error\n%s", output)
	}
}

func TestVersionedFlowImportsValidationFailsForUnsupportedPreparedClientProvider(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "flowRegistryClients.enabled=true",
		"--set", "flowRegistryClients.clients[0].name=github-flows",
		"--set", "flowRegistryClients.clients[0].provider=gitlab",
		"--set", "flowRegistryClients.clients[0].gitlab.apiUrl=https://gitlab.example.com/api/v4",
		"--set", "flowRegistryClients.clients[0].repository.namespace=example-org",
		"--set", "flowRegistryClients.clients[0].repository.name=nifi-flows",
		"--set", "flowRegistryClients.clients[0].gitlab.accessTokenSecret.name=gitlab-flow-registry",
		"--set", "flowRegistryClients.clients[0].gitlab.accessTokenSecret.key=token",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for non-GitHub prepared client provider\n%s", output)
	}
	if !strings.Contains(output, `versionedFlowImports.imports[0].registryClientName="github-flows" currently requires flowRegistryClients.clients[].provider=github or nifiRegistry for bounded runtime-managed import`) {
		t.Fatalf("expected provider validation error\n%s", output)
	}
}

func TestVersionedFlowImportsValidationFailsForUnsupportedPreparedClientAuthType(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "flowRegistryClients.enabled=true",
		"--set", "flowRegistryClients.clients[0].name=github-flows",
		"--set", "flowRegistryClients.clients[0].provider=github",
		"--set", "flowRegistryClients.clients[0].repository.owner=example-org",
		"--set", "flowRegistryClients.clients[0].repository.name=nifi-flows",
		"--set", "flowRegistryClients.clients[0].github.auth.type=appInstallation",
		"--set", "flowRegistryClients.clients[0].github.auth.appId=1234",
		"--set", "flowRegistryClients.clients[0].github.auth.installationId=5678",
		"--set", "flowRegistryClients.clients[0].github.auth.privateKeySecret.name=github-app",
		"--set", "flowRegistryClients.clients[0].github.auth.privateKeySecret.key=privateKey",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=github-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unsupported GitHub appInstallation auth\n%s", output)
	}
	if !strings.Contains(output, `versionedFlowImports.imports[0].registryClientName="github-flows" currently supports github.auth.type none or personalAccessToken; appInstallation remains future work`) {
		t.Fatalf("expected auth-type validation error\n%s", output)
	}
}

func TestVersionedFlowImportsAllowsNiFiRegistryPreparedClientProvider(t *testing.T) {
	args := append(
		preparedNiFiRegistryFlowRegistryClientArgs(),
		"--set", "auth.mode=singleUser",
		"--set", "authz.bundles.flowVersionManager.includeInitialAdmin=true",
		"--set", "versionedFlowImports.enabled=true",
		"--set", "versionedFlowImports.imports[0].name=payments",
		"--set", "versionedFlowImports.imports[0].registryClientName=nifi-registry-flows",
		"--set", "versionedFlowImports.imports[0].bucket=team-a",
		"--set", "versionedFlowImports.imports[0].flowName=payments-api",
		"--set", "versionedFlowImports.imports[0].version=latest",
		"--set", "versionedFlowImports.imports[0].target.rootProcessGroupName=payments-root",
	)
	output, err := helmTemplate(
		t,
		"charts/nifi",
		args...,
	)
	if err != nil {
		t.Fatalf("expected helm template to allow provider=nifiRegistry for bounded runtime-managed import: %v\n%s", err, output)
	}
	for _, want := range []string{
		`"provider": "nifiRegistry"`,
		`"url": "http://nifi-registry.nifi.svc.cluster.local:18080"`,
		`"createsFlowRegistryClientsInNiFi": true`,
		`"requiresLiveRegistryClient": false`,
		`"snapshotSource": "nifi-flow-registry-api-with-bounded-runtime-managed-nifi-registry-client"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedVersionedFlowImportExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-versioned-flow-import-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected versionedFlowImports example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-versioned-flow-imports",
		`"catalogMode": "runtime-managed"`,
		`"name": "payments-api"`,
		`"registryClientRef": {`,
		`"name": "github-flows"`,
		`"flowName": "payments-api"`,
		`"version": "latest"`,
		`"rootProcessGroupName": "payments-api-root"`,
		`"name": "payments-runtime"`,
		`versioned-flow-imports-bootstrap.log`,
		`python3 /opt/nifi/fabric/versioned-flow-imports/bootstrap.py --once`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedRestoreWorkflowExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-restore-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform managed restore workflow example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-flow-registry-clients",
		"name: test-nifi-parameter-contexts",
		"name: test-nifi-versioned-flow-imports",
		`"catalogMode": "runtime-managed"`,
		`"name": "github-flows-restore"`,
		`"name": "payments-runtime"`,
		`"name": "payments-catalog-selection"`,
		`"rootProcessGroupName": "payments-imported-root"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform restore output to contain %q\n%s", want, output)
		}
	}
}

func TestGitHubVersionedFlowSelectionKindExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/nifi-2.8.0-values.yaml",
		"-f", "examples/github-flow-registry-kind-values.yaml",
		"-f", "examples/github-flow-registry-workflow-values.yaml",
		"-f", "examples/github-versioned-flow-selection-kind-values.yaml",
		"-f", "examples/test-fast-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected GitHub versioned flow selection kind example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-versioned-flow-imports",
		`"catalogMode": "runtime-managed"`,
		`"name": "payments-catalog-selection"`,
		`"name": "github-flows-kind"`,
		`"flowName": "catalog-selected-flow"`,
		`"rootProcessGroupName": "payments-imported-root"`,
		`"name": "payments-runtime"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedVersionedFlowImportKindExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-fast-values.yaml",
		"-f", "examples/platform-managed-versioned-flow-import-values.yaml",
		"-f", "examples/platform-managed-versioned-flow-import-kind-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected platform managed versioned flow import kind example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"name: test-nifi-versioned-flow-imports",
		`image: "apache/nifi:2.8.0"`,
		`replicas: 1`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNiFiRegistryFlowRegistryKindExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/nifi-2.8.0-values.yaml",
		"-f", "examples/nifi-registry-flow-registry-kind-values.yaml",
		"-f", "examples/test-fast-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected NiFi Registry Flow Registry kind example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`name: test-nifi-flow-registry-clients`,
		`"name": "nifi-registry-flows-kind"`,
		`"provider": "nifiRegistry"`,
		`"url": "http://nifi-registry.nifi.svc.cluster.local:18080"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestPlatformManagedVersionedFlowImportNiFiRegistryExampleRenders(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi-platform",
		"-f", "examples/platform-managed-values.yaml",
		"-f", "examples/platform-managed-versioned-flow-import-nifi-registry-values.yaml",
	)
	if err != nil {
		t.Fatalf("expected NiFi Registry versionedFlowImports example render to succeed: %v\n%s", err, output)
	}
	for _, want := range []string{
		`name: test-nifi-versioned-flow-imports`,
		`"name": "payments-api"`,
		`"name": "nifi-registry-flows"`,
		`"provider": "nifiRegistry"`,
		`"createsFlowRegistryClientsInNiFi": true`,
		`"flowName": "payments-api"`,
		`"rootProcessGroupName": "payments-api-root"`,
		`"url": client["nifiRegistry"]["url"]`,
		`def fetch_nifi_registry_snapshot(config, client_name, bucket_id, flow_id, resolved_version):`,
		`def selected_snapshot(config, registry_name, registry_id, bucket_name, bucket_id, flow_name, flow_id, version_entry, selected_version):`,
		`only prepared GitHub and NiFi Registry fallback are supported for non-inline snapshot retrieval in this slice`,
		`"ssl-context-service"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered platform output to contain %q\n%s", want, output)
		}
	}
}
