{{- define "nifi.authMode" -}}
{{- $mode := default "singleUser" .Values.auth.mode -}}
{{- if and (ne $mode "singleUser") (ne $mode "ldap") (ne $mode "oidc") -}}
{{- fail "auth.mode must be one of: singleUser, ldap, oidc" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "nifi.authValidation" -}}
{{- $authMode := include "nifi.authMode" . -}}
{{- $authzMode := include "nifi.authzMode" . -}}
{{- $seededGroups := fromYamlArray (include "nifi.baseGroupNamesYaml" .) -}}
{{- if eq $authMode "oidc" -}}
  {{- if not .Values.auth.oidc.discoveryUrl -}}
    {{- fail "auth.oidc.discoveryUrl is required when auth.mode=oidc" -}}
  {{- end -}}
  {{- if not .Values.auth.oidc.clientId -}}
    {{- fail "auth.oidc.clientId is required when auth.mode=oidc" -}}
  {{- end -}}
  {{- if not .Values.auth.oidc.clientSecret.existingSecret -}}
    {{- fail "auth.oidc.clientSecret.existingSecret is required when auth.mode=oidc" -}}
  {{- end -}}
  {{- if not .Values.auth.oidc.claims.identifyingUser -}}
    {{- fail "auth.oidc.claims.identifyingUser is required when auth.mode=oidc" -}}
  {{- end -}}
  {{- if not .Values.auth.oidc.claims.groups -}}
    {{- fail "auth.oidc.claims.groups is required when auth.mode=oidc and must match the token groups claim name" -}}
  {{- end -}}
  {{- if and (eq $authzMode "externalClaimGroups") (eq (len .Values.authz.applicationGroups) 0) (not .Values.authz.bootstrap.initialAdminGroup) -}}
    {{- fail "oidc + externalClaimGroups requires authz.applicationGroups and/or authz.bootstrap.initialAdminGroup so token groups can match seeded NiFi groups" -}}
  {{- end -}}
  {{- if and .Values.ingress.enabled (eq (len .Values.web.proxyHosts) 0) -}}
    {{- fail "auth.mode=oidc with ingress.enabled requires web.proxyHosts to include the external HTTPS host[:port] used for browser redirects" -}}
  {{- end -}}
  {{- if and .Values.openshift.route.enabled .Values.openshift.route.host (eq (len .Values.web.proxyHosts) 0) -}}
    {{- fail "auth.mode=oidc with openshift.route.host requires web.proxyHosts to include the public Route host[:port]" -}}
  {{- end -}}
{{- end -}}
{{- if eq $authMode "ldap" -}}
  {{- if not .Values.auth.ldap.url -}}
    {{- fail "auth.ldap.url is required when auth.mode=ldap" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.managerSecret.name -}}
    {{- fail "auth.ldap.managerSecret.name is required when auth.mode=ldap" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.userSearch.base -}}
    {{- fail "auth.ldap.userSearch.base is required when auth.mode=ldap" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.userSearch.filter -}}
    {{- fail "auth.ldap.userSearch.filter is required when auth.mode=ldap" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.groupSearch.base -}}
    {{- fail "auth.ldap.groupSearch.base is required when auth.mode=ldap and authz.mode=ldapSync" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.groupSearch.nameAttribute -}}
    {{- fail "auth.ldap.groupSearch.nameAttribute is required when auth.mode=ldap and authz.mode=ldapSync" -}}
  {{- end -}}
  {{- if not .Values.auth.ldap.groupSearch.memberAttribute -}}
    {{- fail "auth.ldap.groupSearch.memberAttribute is required when auth.mode=ldap and authz.mode=ldapSync" -}}
  {{- end -}}
{{- end -}}
{{- if and (or (eq $authMode "oidc") (eq $authMode "ldap")) (not .Values.authz.bootstrap.initialAdminGroup) (not .Values.authz.bootstrap.initialAdminIdentity) -}}
  {{- fail "enterprise auth modes require authz.bootstrap.initialAdminGroup or authz.bootstrap.initialAdminIdentity so the first admin path is explicit" -}}
{{- end -}}
{{- range $index, $policy := .Values.authz.policies -}}
  {{- if not $policy.resource -}}
    {{- fail (printf "authz.policies[%d].resource is required" $index) -}}
  {{- end -}}
  {{- if eq (len $policy.actions) 0 -}}
    {{- fail (printf "authz.policies[%d].actions must contain at least one action" $index) -}}
  {{- end -}}
  {{- if eq (len $policy.groups) 0 -}}
    {{- fail (printf "authz.policies[%d].groups must contain at least one NiFi group name" $index) -}}
  {{- end -}}
  {{- if eq $authzMode "externalClaimGroups" -}}
    {{- range $group := $policy.groups -}}
      {{- if not (has $group $seededGroups) -}}
        {{- fail (printf "authz.policies[%d].groups contains %q, but oidc + externalClaimGroups requires every policy group to be present in authz.applicationGroups or authz.bootstrap.initialAdminGroup" $index $group) -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{- define "nifi.authzMode" -}}
{{- $mode := default "fileManaged" .Values.authz.mode -}}
{{- if and (ne $mode "fileManaged") (ne $mode "ldapSync") (ne $mode "externalClaimGroups") -}}
{{- fail "authz.mode must be one of: fileManaged, ldapSync, externalClaimGroups" -}}
{{- end -}}
{{- $authMode := include "nifi.authMode" . -}}
{{- if and (eq $authMode "singleUser") (ne $mode "fileManaged") -}}
{{- fail "singleUser auth currently supports only authz.mode=fileManaged" -}}
{{- end -}}
{{- if and (eq $authMode "oidc") (ne $mode "externalClaimGroups") -}}
{{- fail "oidc auth currently supports only authz.mode=externalClaimGroups" -}}
{{- end -}}
{{- if and (eq $authMode "ldap") (ne $mode "ldapSync") -}}
{{- fail "ldap auth currently supports only authz.mode=ldapSync" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "nifi.singleUserUsernameSecretName" -}}
{{- required "auth.singleUser.existingSecret is required when auth.mode=singleUser" .Values.auth.singleUser.existingSecret -}}
{{- end -}}

{{- define "nifi.oidcClientSecretName" -}}
{{- required "auth.oidc.clientSecret.existingSecret is required when auth.mode=oidc" .Values.auth.oidc.clientSecret.existingSecret -}}
{{- end -}}

{{- define "nifi.oidcClientSecretKey" -}}
{{- default "clientSecret" .Values.auth.oidc.clientSecret.key -}}
{{- end -}}

{{- define "nifi.ldapManagerSecretName" -}}
{{- required "auth.ldap.managerSecret.name is required when auth.mode=ldap" .Values.auth.ldap.managerSecret.name -}}
{{- end -}}

{{- define "nifi.xmlEscape" -}}
{{- $value := default "" . -}}
{{- $value = replace "&" "&amp;" $value -}}
{{- $value = replace "<" "&lt;" $value -}}
{{- $value = replace ">" "&gt;" $value -}}
{{- $value = replace "\"" "&quot;" $value -}}
{{- $value = replace "'" "&apos;" $value -}}
{{- $value -}}
{{- end -}}

{{- define "nifi.stableId" -}}
{{- $sum := sha256sum (printf "%v" .) -}}
{{- printf "%s-%s-%s-%s-%s" (substr 0 8 $sum) (substr 8 12 $sum) (substr 12 16 $sum) (substr 16 20 $sum) (substr 20 32 $sum) -}}
{{- end -}}

{{- define "nifi.dynamicAdminIdentityPlaceholder" -}}
{{- if .Values.authz.bootstrap.initialAdminGroup -}}
{{- "" -}}
{{- else if eq (include "nifi.authMode" .) "singleUser" -}}
__SINGLE_USER_IDENTITY__
{{- else -}}
{{ include "nifi.xmlEscape" .Values.authz.bootstrap.initialAdminIdentity }}
{{- end -}}
{{- end -}}

{{- define "nifi.adminIdentityForFiles" -}}
{{- if .Values.authz.bootstrap.initialAdminGroup -}}
{{- "" -}}
{{- else if .Values.authz.bootstrap.initialAdminIdentity -}}
{{ include "nifi.xmlEscape" .Values.authz.bootstrap.initialAdminIdentity }}
{{- else if eq (include "nifi.authMode" .) "singleUser" -}}
__SINGLE_USER_IDENTITY__
{{- else -}}
{{- "" -}}
{{- end -}}
{{- end -}}

{{- define "nifi.baseGroupNamesYaml" -}}
{{- $groups := list -}}
{{- range .Values.authz.applicationGroups }}
{{- $groups = append $groups . -}}
{{- end -}}
{{- if .Values.authz.bootstrap.initialAdminGroup }}
{{- $groups = append $groups .Values.authz.bootstrap.initialAdminGroup -}}
{{- end -}}
{{- toYaml (uniq $groups) -}}
{{- end -}}

{{- define "nifi.fileUserGroupProviderIdentifier" -}}
file-user-group-provider
{{- end -}}

{{- define "nifi.activeUserGroupProviderIdentifier" -}}
{{- if eq (include "nifi.authzMode" .) "ldapSync" -}}
composite-configurable-user-group-provider
{{- else -}}
file-user-group-provider
{{- end -}}
{{- end -}}

{{- define "nifi.usersXml" -}}
{{- $ldapSync := eq (include "nifi.authzMode" .) "ldapSync" -}}
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<tenants>
    <groups>
{{- if not $ldapSync }}
{{- range (fromYamlArray (include "nifi.baseGroupNamesYaml" .)) }}
        <group identifier="{{ include "nifi.stableId" (printf "group:%s" .) }}" name="{{ include "nifi.xmlEscape" . }}"/>
{{- end }}
{{- end }}
    </groups>
    <users>
        <user identifier="__NODE_IDENTITY_ID__" identity="__NODE_IDENTITY__"/>
{{- $adminIdentity := include "nifi.adminIdentityForFiles" . -}}
{{- if $adminIdentity }}
        <user identifier="__ADMIN_IDENTITY_ID__" identity="{{ $adminIdentity }}"/>
{{- end }}
    </users>
</tenants>
{{- end -}}

{{- define "nifi.adminBindingXml" -}}
{{- if .Values.authz.bootstrap.initialAdminGroup }}
            <group identifier="{{ include "nifi.stableId" (printf "group:%s" .Values.authz.bootstrap.initialAdminGroup) }}"/>
{{- else if (include "nifi.adminIdentityForFiles" .) }}
            <user identifier="__ADMIN_IDENTITY_ID__"/>
{{- end -}}
{{- end -}}

{{- define "nifi.adminGroupBindingXml" -}}
{{- if .Values.authz.bootstrap.initialAdminGroup }}
            <group identifier="{{ include "nifi.stableId" (printf "group:%s" .Values.authz.bootstrap.initialAdminGroup) }}"/>
{{- end -}}
{{- end -}}

{{- define "nifi.adminUserBindingXml" -}}
{{- if and (not .Values.authz.bootstrap.initialAdminGroup) (include "nifi.adminIdentityForFiles" .) }}
            <user identifier="__ADMIN_IDENTITY_ID__"/>
{{- end -}}
{{- end -}}

{{- define "nifi.baseAdminPoliciesYaml" -}}
{{- toYaml (list
  (dict "resource" "/flow" "action" "R")
  (dict "resource" "/restricted-components" "action" "W")
  (dict "resource" "/tenants" "action" "R")
  (dict "resource" "/tenants" "action" "W")
  (dict "resource" "/policies" "action" "R")
  (dict "resource" "/policies" "action" "W")
  (dict "resource" "/controller" "action" "R")
  (dict "resource" "/controller" "action" "W")) -}}
{{- end -}}

{{- define "nifi.baseAdminPolicyKeysYaml" -}}
{{- $keys := list -}}
{{- range $policy := (fromYamlArray (include "nifi.baseAdminPoliciesYaml" .)) }}
{{- $keys = append $keys (printf "%s|%s" $policy.resource $policy.action) -}}
{{- end }}
{{- toYaml $keys -}}
{{- end -}}

{{- define "nifi.authorizationsXml" -}}
{{- if eq (include "nifi.authzMode" .) "ldapSync" -}}
{{- printf "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" -}}
<authorizations>
    <policies>
    </policies>
</authorizations>
{{- else -}}
{{- $baseAdminPolicyKeys := fromYamlArray (include "nifi.baseAdminPolicyKeysYaml" .) -}}
{{- $bootstrapAdminGroup := .Values.authz.bootstrap.initialAdminGroup -}}
{{- $policySpecs := dict -}}
{{- if or .Values.authz.bootstrap.initialAdminGroup (include "nifi.adminIdentityForFiles" .) }}
{{- range $policy := (fromYamlArray (include "nifi.baseAdminPoliciesYaml" .)) }}
{{- $key := printf "%s|%s" $policy.resource $policy.action -}}
{{- $_ := set $policySpecs $key (dict "resource" $policy.resource "action" $policy.action "includeAdmin" true "includeNode" false "groups" (list)) -}}
{{- end }}
{{- end }}
{{- range $policy := (list (dict "resource" "/controller" "action" "R") (dict "resource" "/proxy" "action" "W")) }}
{{- $key := printf "%s|%s" $policy.resource $policy.action -}}
{{- $entry := get $policySpecs $key | default (dict "resource" $policy.resource "action" $policy.action "includeAdmin" false "includeNode" false "groups" (list)) -}}
{{- $_ := set $entry "includeNode" true -}}
{{- $_ := set $policySpecs $key $entry -}}
{{- end }}
{{- range .Values.authz.policies }}
{{- $policy := . -}}
{{- $resource := required "authz.policies[].resource is required" $policy.resource -}}
{{- range $action := $policy.actions }}
{{- $key := printf "%s|%s" $resource $action -}}
{{- $entry := get $policySpecs $key | default (dict "resource" $resource "action" $action "includeAdmin" false "includeNode" false "groups" (list)) -}}
{{- $policyGroups := default (list) (get $entry "groups") -}}
{{- range $group := $policy.groups }}
{{- if and $bootstrapAdminGroup (eq $group $bootstrapAdminGroup) (has (printf "%s|%s" $resource $action) $baseAdminPolicyKeys) -}}
{{- else -}}
{{- $policyGroups = append $policyGroups $group -}}
{{- end -}}
{{- end }}
{{- $_ := set $entry "groups" (uniq $policyGroups) -}}
{{- $_ := set $policySpecs $key $entry -}}
{{- end }}
{{- end }}
{{- printf "<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"yes\"?>\n" -}}
<authorizations>
    <policies>
{{- range $key := (keys $policySpecs | sortAlpha) }}
{{- $entry := get $policySpecs $key -}}
{{- $policyGroups := sortAlpha (uniq (default (list) (get $entry "groups"))) -}}
{{- if or (get $entry "includeAdmin") (get $entry "includeNode") (gt (len $policyGroups) 0) }}
        <policy identifier="{{ include "nifi.stableId" (printf "policy:%s:%s:admin=%t:node=%t:groups=%v" (get $entry "resource") (get $entry "action") (get $entry "includeAdmin") (get $entry "includeNode") $policyGroups) }}" resource="{{ get $entry "resource" }}" action="{{ get $entry "action" }}">
{{- if get $entry "includeAdmin" }}
{{ include "nifi.adminGroupBindingXml" $ }}
{{- end }}
{{- range $group := $policyGroups }}
            <group identifier="{{ include "nifi.stableId" (printf "group:%s" $group) }}"/>
{{- end }}
{{- if get $entry "includeNode" }}
            <user identifier="__NODE_IDENTITY_ID__"/>
{{- end }}
{{- if get $entry "includeAdmin" }}
{{ include "nifi.adminUserBindingXml" $ }}
{{- end }}
        </policy>
{{- end }}
{{- end }}
    </policies>
</authorizations>
{{- end -}}
{{- end -}}

{{- define "nifi.authorizersXml" -}}
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<authorizers>
    <userGroupProvider>
        <identifier>{{ include "nifi.fileUserGroupProviderIdentifier" . }}</identifier>
        <class>org.apache.nifi.authorization.FileUserGroupProvider</class>
        <property name="Users File">./conf/users.xml</property>
{{- if and (eq (include "nifi.authzMode" .) "ldapSync") .Values.authz.bootstrap.initialAdminIdentity }}
        <property name="Initial User Identity 1">{{ include "nifi.xmlEscape" .Values.authz.bootstrap.initialAdminIdentity }}</property>
{{- end }}
{{- if eq (include "nifi.authzMode" .) "ldapSync" }}
        <property name="Initial User Identity 2">__NODE_IDENTITY__</property>
{{- end }}
    </userGroupProvider>
{{- if eq (include "nifi.authzMode" .) "ldapSync" }}
    <userGroupProvider>
        <identifier>ldap-user-group-provider</identifier>
        <class>org.apache.nifi.ldap.tenants.LdapUserGroupProvider</class>
        <property name="Authentication Strategy">{{ .Values.auth.ldap.authenticationStrategy }}</property>
        <property name="Manager DN">__LDAP_MANAGER_DN__</property>
        <property name="Manager Password">__LDAP_MANAGER_PASSWORD__</property>
        <property name="TLS - Keystore">{{ .Values.tls.mountPath }}/{{ .Values.tls.keystoreKey }}</property>
        <property name="TLS - Keystore Password">__KEYSTORE_PASSWORD__</property>
        <property name="TLS - Keystore Type">PKCS12</property>
        <property name="TLS - Truststore">__TRUSTSTORE_PATH__</property>
        <property name="TLS - Truststore Password">__TRUSTSTORE_PASSWORD__</property>
        <property name="TLS - Truststore Type">PKCS12</property>
        <property name="TLS - Client Auth">{{ .Values.auth.ldap.tls.clientAuth }}</property>
        <property name="TLS - Protocol">{{ .Values.auth.ldap.tls.protocol }}</property>
        <property name="TLS - Shutdown Gracefully">{{ ternary "true" "false" .Values.auth.ldap.tls.shutdownGracefully }}</property>
        <property name="Referral Strategy">{{ .Values.auth.ldap.referralStrategy }}</property>
        <property name="Connect Timeout">{{ .Values.auth.ldap.connectTimeout }}</property>
        <property name="Read Timeout">{{ .Values.auth.ldap.readTimeout }}</property>
        <property name="Url">{{ required "auth.ldap.url is required when auth.mode=ldap" .Values.auth.ldap.url }}</property>
        <property name="Page Size">{{ .Values.auth.ldap.pageSize }}</property>
        <property name="Sync Interval">{{ .Values.auth.ldap.syncInterval }}</property>
        <property name="Group Membership - Enforce Case Sensitivity">{{ ternary "true" "false" .Values.auth.ldap.groupMembershipEnforceCaseSensitivity }}</property>
        <property name="User Search Base">{{ required "auth.ldap.userSearch.base is required when auth.mode=ldap" .Values.auth.ldap.userSearch.base }}</property>
        <property name="User Object Class">{{ .Values.auth.ldap.userSearch.objectClass }}</property>
        <property name="User Search Scope">{{ .Values.auth.ldap.userSearch.scope }}</property>
        <property name="User Search Filter">{{ .Values.auth.ldap.userSearch.filter }}</property>
        <property name="User Identity Attribute">{{ .Values.auth.ldap.userSearch.identityAttribute }}</property>
        <property name="User Group Name Attribute">{{ .Values.auth.ldap.userSearch.groupNameAttribute }}</property>
        <property name="User Group Name Attribute - Referenced Group Attribute">{{ .Values.auth.ldap.userSearch.groupNameReferencedGroupAttribute }}</property>
        <property name="Group Search Base">{{ .Values.auth.ldap.groupSearch.base }}</property>
        <property name="Group Object Class">{{ .Values.auth.ldap.groupSearch.objectClass }}</property>
        <property name="Group Search Scope">{{ .Values.auth.ldap.groupSearch.scope }}</property>
        <property name="Group Search Filter">{{ .Values.auth.ldap.groupSearch.filter }}</property>
        <property name="Group Name Attribute">{{ .Values.auth.ldap.groupSearch.nameAttribute }}</property>
        <property name="Group Member Attribute">{{ .Values.auth.ldap.groupSearch.memberAttribute }}</property>
        <property name="Group Member Attribute - Referenced User Attribute">{{ .Values.auth.ldap.groupSearch.memberReferencedUserAttribute }}</property>
    </userGroupProvider>
    <userGroupProvider>
        <identifier>composite-configurable-user-group-provider</identifier>
        <class>org.apache.nifi.authorization.CompositeConfigurableUserGroupProvider</class>
        <property name="Configurable User Group Provider">{{ include "nifi.fileUserGroupProviderIdentifier" . }}</property>
        <property name="User Group Provider 1">ldap-user-group-provider</property>
    </userGroupProvider>
{{- end }}
    <accessPolicyProvider>
        <identifier>file-access-policy-provider</identifier>
        <class>org.apache.nifi.authorization.FileAccessPolicyProvider</class>
        <property name="User Group Provider">{{ include "nifi.activeUserGroupProviderIdentifier" . }}</property>
        <property name="Authorizations File">./conf/authorizations.xml</property>
        <property name="Initial Admin Identity">{{ include "nifi.dynamicAdminIdentityPlaceholder" . }}</property>
        <property name="Initial Admin Group">{{ include "nifi.xmlEscape" .Values.authz.bootstrap.initialAdminGroup }}</property>
        <property name="Node Identity 1">__NODE_IDENTITY__</property>
        <property name="Node Group"></property>
    </accessPolicyProvider>
    <authorizer>
        <identifier>managed-authorizer</identifier>
        <class>org.apache.nifi.authorization.StandardManagedAuthorizer</class>
        <property name="Access Policy Provider">file-access-policy-provider</property>
    </authorizer>
</authorizers>
{{- end -}}

{{- define "nifi.loginIdentityProvidersXml" -}}
{{- $authMode := include "nifi.authMode" . -}}
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<loginIdentityProviders>
{{- if eq $authMode "singleUser" }}
    <provider>
        <identifier>single-user-provider</identifier>
        <class>org.apache.nifi.authentication.single.user.SingleUserLoginIdentityProvider</class>
        <property name="Username"/>
        <property name="Password"/>
    </provider>
{{- end }}
{{- if eq $authMode "ldap" }}
    <provider>
        <identifier>ldap-provider</identifier>
        <class>org.apache.nifi.ldap.LdapProvider</class>
        <property name="Authentication Strategy">{{ .Values.auth.ldap.authenticationStrategy }}</property>
        <property name="Manager DN">__LDAP_MANAGER_DN__</property>
        <property name="Manager Password">__LDAP_MANAGER_PASSWORD__</property>
        <property name="TLS - Keystore">{{ .Values.tls.mountPath }}/{{ .Values.tls.keystoreKey }}</property>
        <property name="TLS - Keystore Password">__KEYSTORE_PASSWORD__</property>
        <property name="TLS - Keystore Type">PKCS12</property>
        <property name="TLS - Truststore">__TRUSTSTORE_PATH__</property>
        <property name="TLS - Truststore Password">__TRUSTSTORE_PASSWORD__</property>
        <property name="TLS - Truststore Type">PKCS12</property>
        <property name="TLS - Client Auth">{{ .Values.auth.ldap.tls.clientAuth }}</property>
        <property name="TLS - Protocol">{{ .Values.auth.ldap.tls.protocol }}</property>
        <property name="TLS - Shutdown Gracefully">{{ ternary "true" "false" .Values.auth.ldap.tls.shutdownGracefully }}</property>
        <property name="Referral Strategy">{{ .Values.auth.ldap.referralStrategy }}</property>
        <property name="Connect Timeout">{{ .Values.auth.ldap.connectTimeout }}</property>
        <property name="Read Timeout">{{ .Values.auth.ldap.readTimeout }}</property>
        <property name="Url">{{ .Values.auth.ldap.url }}</property>
        <property name="User Search Base">{{ .Values.auth.ldap.userSearch.base }}</property>
        <property name="User Search Filter">{{ .Values.auth.ldap.userSearch.filter }}</property>
        <property name="Identity Strategy">{{ .Values.auth.ldap.identityStrategy }}</property>
        <property name="Authentication Expiration">{{ .Values.auth.ldap.authenticationExpiration }}</property>
    </provider>
{{- end }}
</loginIdentityProviders>
{{- end -}}
