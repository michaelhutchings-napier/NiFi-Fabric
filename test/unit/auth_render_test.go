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

func TestOIDCGroupClaimsExampleRendersCustomPolicyBindings(t *testing.T) {
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

	for _, want := range []string{
		`resource="/flow" action="R"`,
		`resource="/controller" action="R"`,
		`resource="/controller" action="W"`,
		`<group identifier="0881bb71-9b50-c83c-08ac-ada65cb2cef2"/>`,
		`<group identifier="1163e273-26f6-87ef-bd86-1506e7c3e6c7"/>`,
		`<group identifier="9816433e-d325-e655-6542-191408365a81"/>`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestOIDCInitialAdminGroupKindExampleKeepsGroupBootstrapPrimary(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-initial-admin-group-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, `<property name="Initial Admin Group">nifi-platform-admins</property>`) {
		t.Fatalf("expected rendered output to keep Initial Admin Group as the primary bootstrap path\n%s", output)
	}
	if strings.Contains(output, `<property name="Initial Admin Identity">alice@example.com</property>`) {
		t.Fatalf("expected rendered output not to fall back to Initial Admin Identity in the Initial Admin Group proof profile\n%s", output)
	}
}

func TestOIDCInitialAdminGroupFastProfileKeepsSingleReplica(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/test-fast-values.yaml",
		"-f", "examples/oidc-kind-initial-admin-group-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	statefulSetStart := strings.Index(output, "kind: StatefulSet")
	if statefulSetStart == -1 {
		t.Fatalf("expected rendered output to contain the NiFi StatefulSet\n%s", output)
	}
	statefulSetBody := output[statefulSetStart:]
	if !strings.Contains(statefulSetBody, "\n  replicas: 1\n") {
		t.Fatalf("expected the Initial Admin Group fast profile to keep the NiFi StatefulSet at one replica\n%s", statefulSetBody)
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

func TestMutableFlowAuthzExampleRendersRootProcessGroupBootstrapPolicies(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/mutable-flow-authz-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="R"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`,
		`resource="/flow" action="R"`,
		`resource="/controller" action="R"`,
		`Bootstrapping mutable flow authorizations from the discovered root process group id`,
		`authorizations.template.xml`,
		`__ROOT_PROCESS_GROUP_ID__`,
		`root_group.get("instanceIdentifier")`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestMutableFlowAuthzRejectsLdapSync(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/ldap-values.yaml",
		"-f", "examples/mutable-flow-authz-values.yaml",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for ldapSync mutableFlow\n%s", output)
	}
	if !strings.Contains(output, "authz.capabilities.mutableFlow is not supported when authz.mode=ldapSync") {
		t.Fatalf("expected ldapSync validation failure in output\n%s", output)
	}
}

func TestMutableFlowAuthzSupportsOIDCExternalClaimGroups(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-values.yaml",
		"--set", "authz.capabilities.mutableFlow.enabled=true",
		"--set", "authz.capabilities.mutableFlow.groups[0]=nifi-flow-operators",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="R"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`,
		`<group identifier="1163e273-26f6-87ef-bd86-1506e7c3e6c7"/>`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestMutableFlowAuthzRejectsUnknownTargetGroup(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"--set", "authz.capabilities.mutableFlow.enabled=true",
		"--set", "authz.capabilities.mutableFlow.includeInitialAdmin=false",
		"--set", "authz.capabilities.mutableFlow.groups[0]=missing-group",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unknown mutableFlow group\n%s", output)
	}
	if !strings.Contains(output, `authz.capabilities.mutableFlow.groups[0] contains "missing-group"`) {
		t.Fatalf("expected unknown-group validation failure in output\n%s", output)
	}
}

func TestMutableFlowAuthzRejectsEmptyTargets(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"--set", "authz.capabilities.mutableFlow.enabled=true",
		"--set", "authz.capabilities.mutableFlow.includeInitialAdmin=false",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail when mutableFlow has no targets\n%s", output)
	}
	if !strings.Contains(output, "authz.capabilities.mutableFlow requires at least one target group or includeInitialAdmin=true") {
		t.Fatalf("expected empty-target validation failure in output\n%s", output)
	}
}

func TestNamedPolicyBundlesRenderExpectedPolicies(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"--set", "authz.applicationGroups[0]=nifi-viewers",
		"--set", "authz.applicationGroups[1]=nifi-editors",
		"--set", "authz.applicationGroups[2]=nifi-version-managers",
		"--set", "authz.bundles.viewer.groups[0]=nifi-viewers",
		"--set", "authz.bundles.editor.groups[0]=nifi-editors",
		"--set", "authz.bundles.flowVersionManager.groups[0]=nifi-version-managers",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`resource="/flow" action="R"`,
		`resource="/controller" action="R"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="R"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestSingleUserFileManagedAuthSeedsInitialAdminIdentityPlaceholder(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`<property name="Initial Admin Identity">__SINGLE_USER_IDENTITY__</property>`,
		`<user identifier="__ADMIN_IDENTITY_ID__" identity="__SINGLE_USER_IDENTITY__"/>`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNamedPolicyBundlesSupportOIDCExternalClaimGroups(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-values.yaml",
		"--set", "authz.bundles.viewer.groups[0]=nifi-flow-observers",
		"--set", "authz.bundles.editor.groups[0]=nifi-flow-operators",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`<group identifier="0881bb71-9b50-c83c-08ac-ada65cb2cef2"/>`,
		`<group identifier="1163e273-26f6-87ef-bd86-1506e7c3e6c7"/>`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestOIDCAdditionalTrustBundleDefaultsTruststoreStrategyToNIFI(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-values.yaml",
		"--set", "tls.additionalTrustBundle.enabled=true",
		"--set", "tls.additionalTrustBundle.secretRef.name=oidc-ca",
		"--set", "tls.additionalTrustBundle.secretRef.key=ca.crt",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	if !strings.Contains(output, "nifi.security.user.oidc.truststore.strategy=NIFI") {
		t.Fatalf("expected rendered output to default OIDC truststore strategy to NIFI when an extra trust bundle is enabled\n%s", output)
	}
}

func TestOIDCAdditionalTrustBundleAllowsExplicitTruststoreStrategyOverride(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/oidc-values.yaml",
		"-f", "examples/oidc-group-claims-values.yaml",
		"-f", "examples/oidc-kind-values.yaml",
		"--set", "tls.additionalTrustBundle.enabled=true",
		"--set", "tls.additionalTrustBundle.secretRef.name=oidc-ca",
		"--set", "tls.additionalTrustBundle.secretRef.key=ca.crt",
		"--set", "auth.oidc.extraProperties.nifi\\.security\\.user\\.oidc\\.truststore\\.strategy=JDK",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	if strings.Contains(output, "nifi.security.user.oidc.truststore.strategy=NIFI") {
		t.Fatalf("expected rendered output to respect an explicit OIDC truststore strategy override\n%s", output)
	}
	if !strings.Contains(output, "nifi.security.user.oidc.truststore.strategy=JDK") {
		t.Fatalf("expected rendered output to include the explicit OIDC truststore strategy override\n%s", output)
	}
}

func TestGitHubWorkflowExampleRendersFlowVersionManagerBundle(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"-f", "examples/nifi-2.8.0-values.yaml",
		"-f", "examples/github-flow-registry-kind-values.yaml",
		"-f", "examples/test-fast-values.yaml",
		"-f", "examples/github-flow-registry-workflow-values.yaml",
	)
	if err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, output)
	}

	for _, want := range []string{
		`"provider": "github"`,
		`"implementationClass": "org.apache.nifi.github.GitHubFlowRegistryClient"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="R"`,
		`resource="/process-groups/__ROOT_PROCESS_GROUP_ID__" action="W"`,
		`Bootstrapping mutable flow authorizations from the discovered root process group id`,
		`replicas: 1`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected rendered output to contain %q\n%s", want, output)
		}
	}
}

func TestNamedPolicyBundlesRejectUnknownTargetGroup(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/managed/values.yaml",
		"--set", "authz.bundles.viewer.groups[0]=missing-group",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for unknown named-bundle group\n%s", output)
	}
	if !strings.Contains(output, `authz.bundles.viewer.groups[0] contains "missing-group"`) {
		t.Fatalf("expected unknown-group validation failure in output\n%s", output)
	}
}

func TestNamedPolicyBundlesRejectLdapSyncNonAdminBundles(t *testing.T) {
	output, err := helmTemplate(
		t,
		"charts/nifi",
		"-f", "examples/ldap-values.yaml",
		"--set", "authz.bundles.viewer.includeInitialAdmin=true",
	)
	if err == nil {
		t.Fatalf("expected helm template to fail for ldapSync viewer bundle\n%s", output)
	}
	if !strings.Contains(output, "authz.bundles.viewer is not supported when authz.mode=ldapSync") {
		t.Fatalf("expected ldapSync named-bundle validation failure in output\n%s", output)
	}
}
