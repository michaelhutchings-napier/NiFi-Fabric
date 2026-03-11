{{- define "nifi.flowRegistryClientsEnabled" -}}
{{- if .Values.flowRegistryClients.enabled -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "nifi.flowRegistryClientClass" -}}
{{- $provider := .provider -}}
{{- if eq $provider "github" -}}
org.apache.nifi.github.GitHubFlowRegistryClient
{{- else if eq $provider "gitlab" -}}
org.apache.nifi.gitlab.GitLabFlowRegistryClient
{{- else if eq $provider "bitbucket" -}}
org.apache.nifi.atlassian.bitbucket.BitbucketFlowRegistryClient
{{- else if eq $provider "azureDevOps" -}}
org.apache.nifi.azure.devops.AzureDevOpsFlowRegistryClient
{{- end -}}
{{- end -}}

{{- define "nifi.flowRegistryParameterContextValues" -}}
{{- $value := default "retain" . -}}
{{- if eq $value "remove" -}}
REMOVE
{{- else if eq $value "ignoreChanges" -}}
IGNORE_CHANGES
{{- else -}}
RETAIN
{{- end -}}
{{- end -}}

{{- define "nifi.flowRegistryValidation" -}}
{{- if not .Values.flowRegistryClients.enabled -}}
{{- else -}}
  {{- if eq (len .Values.flowRegistryClients.clients) 0 -}}
    {{- fail "flowRegistryClients.enabled=true requires flowRegistryClients.clients to contain at least one client definition" -}}
  {{- end -}}
  {{- $names := list -}}
  {{- range $index, $client := .Values.flowRegistryClients.clients -}}
    {{- if not $client.name -}}
      {{- fail (printf "flowRegistryClients.clients[%d].name is required" $index) -}}
    {{- end -}}
    {{- if has $client.name $names -}}
      {{- fail (printf "flowRegistryClients.clients[%d].name=%q is duplicated; client names must be unique" $index $client.name) -}}
    {{- end -}}
    {{- $names = append $names $client.name -}}
    {{- if and (ne $client.provider "github") (ne $client.provider "gitlab") (ne $client.provider "bitbucket") (ne $client.provider "azureDevOps") -}}
      {{- fail (printf "flowRegistryClients.clients[%d].provider must be one of: github, gitlab, bitbucket, azureDevOps" $index) -}}
    {{- end -}}
    {{- if and $client.repository.path (or (hasPrefix "/" $client.repository.path) (hasSuffix "/" $client.repository.path)) -}}
      {{- fail (printf "flowRegistryClients.clients[%d].repository.path must not start or end with '/'" $index) -}}
    {{- end -}}
    {{- if and $client.parameterContextValues (ne $client.parameterContextValues "retain") (ne $client.parameterContextValues "remove") (ne $client.parameterContextValues "ignoreChanges") -}}
      {{- fail (printf "flowRegistryClients.clients[%d].parameterContextValues must be one of: retain, remove, ignoreChanges" $index) -}}
    {{- end -}}
    {{- if eq $client.provider "github" -}}
      {{- if not $client.repository.owner -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.owner is required for provider=github" $index) -}}
      {{- end -}}
      {{- if not $client.repository.name -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.name is required for provider=github" $index) -}}
      {{- end -}}
      {{- if and $client.github.auth.type (ne $client.github.auth.type "none") (ne $client.github.auth.type "personalAccessToken") (ne $client.github.auth.type "appInstallation") -}}
        {{- fail (printf "flowRegistryClients.clients[%d].github.auth.type must be one of: none, personalAccessToken, appInstallation" $index) -}}
      {{- end -}}
      {{- if or (not $client.github.auth.type) (eq $client.github.auth.type "personalAccessToken") -}}
        {{- if not $client.github.auth.personalAccessTokenSecret.name -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.personalAccessTokenSecret.name is required when provider=github and auth.type is omitted or personalAccessToken" $index) -}}
        {{- end -}}
        {{- if not $client.github.auth.personalAccessTokenSecret.key -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.personalAccessTokenSecret.key is required when provider=github and auth.type is omitted or personalAccessToken" $index) -}}
        {{- end -}}
      {{- end -}}
      {{- if eq $client.github.auth.type "appInstallation" -}}
        {{- if not $client.github.auth.appId -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.appId is required when github.auth.type=appInstallation" $index) -}}
        {{- end -}}
        {{- if not $client.github.auth.installationId -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.installationId is required when github.auth.type=appInstallation" $index) -}}
        {{- end -}}
        {{- if not $client.github.auth.privateKeySecret.name -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.privateKeySecret.name is required when github.auth.type=appInstallation" $index) -}}
        {{- end -}}
        {{- if not $client.github.auth.privateKeySecret.key -}}
          {{- fail (printf "flowRegistryClients.clients[%d].github.auth.privateKeySecret.key is required when github.auth.type=appInstallation" $index) -}}
        {{- end -}}
      {{- end -}}
    {{- end -}}
    {{- if eq $client.provider "gitlab" -}}
      {{- if not $client.gitlab.apiUrl -}}
        {{- fail (printf "flowRegistryClients.clients[%d].gitlab.apiUrl is required for provider=gitlab" $index) -}}
      {{- end -}}
      {{- if not $client.repository.namespace -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.namespace is required for provider=gitlab" $index) -}}
      {{- end -}}
      {{- if not $client.repository.name -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.name is required for provider=gitlab" $index) -}}
      {{- end -}}
      {{- if not $client.gitlab.accessTokenSecret.name -}}
        {{- fail (printf "flowRegistryClients.clients[%d].gitlab.accessTokenSecret.name is required for provider=gitlab" $index) -}}
      {{- end -}}
      {{- if not $client.gitlab.accessTokenSecret.key -}}
        {{- fail (printf "flowRegistryClients.clients[%d].gitlab.accessTokenSecret.key is required for provider=gitlab" $index) -}}
      {{- end -}}
    {{- end -}}
    {{- if eq $client.provider "bitbucket" -}}
      {{- if and (ne $client.bitbucket.formFactor "cloud") (ne $client.bitbucket.formFactor "dataCenter") -}}
        {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.formFactor must be one of: cloud, dataCenter" $index) -}}
      {{- end -}}
      {{- if not $client.bitbucket.apiUrl -}}
        {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.apiUrl is required for provider=bitbucket" $index) -}}
      {{- end -}}
      {{- if not $client.repository.name -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.name is required for provider=bitbucket" $index) -}}
      {{- end -}}
      {{- if eq $client.bitbucket.formFactor "cloud" -}}
        {{- if not $client.repository.workspace -}}
          {{- fail (printf "flowRegistryClients.clients[%d].repository.workspace is required when bitbucket.formFactor=cloud" $index) -}}
        {{- end -}}
      {{- end -}}
      {{- if eq $client.bitbucket.formFactor "dataCenter" -}}
        {{- if not $client.repository.projectKey -}}
          {{- fail (printf "flowRegistryClients.clients[%d].repository.projectKey is required when bitbucket.formFactor=dataCenter" $index) -}}
        {{- end -}}
      {{- end -}}
      {{- if not $client.bitbucket.webClientServiceName -}}
        {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.webClientServiceName is required for provider=bitbucket" $index) -}}
      {{- end -}}
      {{- if and (ne $client.bitbucket.auth.type "accessToken") (ne $client.bitbucket.auth.type "basicAuth") (ne $client.bitbucket.auth.type "oauth2") -}}
        {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.type must be one of: accessToken, basicAuth, oauth2" $index) -}}
      {{- end -}}
      {{- if eq $client.bitbucket.auth.type "accessToken" -}}
        {{- if not $client.bitbucket.auth.accessTokenSecret.name -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.accessTokenSecret.name is required when bitbucket.auth.type=accessToken" $index) -}}
        {{- end -}}
        {{- if not $client.bitbucket.auth.accessTokenSecret.key -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.accessTokenSecret.key is required when bitbucket.auth.type=accessToken" $index) -}}
        {{- end -}}
      {{- end -}}
      {{- if eq $client.bitbucket.auth.type "basicAuth" -}}
        {{- if not $client.bitbucket.auth.username -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.username is required when bitbucket.auth.type=basicAuth" $index) -}}
        {{- end -}}
        {{- if not $client.bitbucket.auth.passwordSecret.name -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.passwordSecret.name is required when bitbucket.auth.type=basicAuth" $index) -}}
        {{- end -}}
        {{- if not $client.bitbucket.auth.passwordSecret.key -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.passwordSecret.key is required when bitbucket.auth.type=basicAuth" $index) -}}
        {{- end -}}
      {{- end -}}
      {{- if eq $client.bitbucket.auth.type "oauth2" -}}
        {{- if not $client.bitbucket.auth.oauth2AccessTokenProviderName -}}
          {{- fail (printf "flowRegistryClients.clients[%d].bitbucket.auth.oauth2AccessTokenProviderName is required when bitbucket.auth.type=oauth2" $index) -}}
        {{- end -}}
      {{- end -}}
    {{- end -}}
    {{- if eq $client.provider "azureDevOps" -}}
      {{- if not $client.azureDevOps.apiUrl -}}
        {{- fail (printf "flowRegistryClients.clients[%d].azureDevOps.apiUrl is required for provider=azureDevOps" $index) -}}
      {{- end -}}
      {{- if not $client.azureDevOps.organization -}}
        {{- fail (printf "flowRegistryClients.clients[%d].azureDevOps.organization is required for provider=azureDevOps" $index) -}}
      {{- end -}}
      {{- if not $client.azureDevOps.project -}}
        {{- fail (printf "flowRegistryClients.clients[%d].azureDevOps.project is required for provider=azureDevOps" $index) -}}
      {{- end -}}
      {{- if not $client.repository.name -}}
        {{- fail (printf "flowRegistryClients.clients[%d].repository.name is required for provider=azureDevOps" $index) -}}
      {{- end -}}
      {{- if not $client.azureDevOps.webClientServiceName -}}
        {{- fail (printf "flowRegistryClients.clients[%d].azureDevOps.webClientServiceName is required for provider=azureDevOps" $index) -}}
      {{- end -}}
      {{- if not $client.azureDevOps.oauth2AccessTokenProviderName -}}
        {{- fail (printf "flowRegistryClients.clients[%d].azureDevOps.oauth2AccessTokenProviderName is required for provider=azureDevOps" $index) -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{- define "nifi.flowRegistryClientDefinitions" -}}
clients:
{{- range $client := .Values.flowRegistryClients.clients }}
  - name: {{ $client.name | quote }}
    provider: {{ $client.provider | quote }}
    implementationClass: {{ include "nifi.flowRegistryClientClass" $client | quote }}
    {{- with $client.description }}
    description: {{ . | quote }}
    {{- end }}
    bootstrapMode: "prepared-only"
    properties:
    {{- if eq $client.provider "github" }}
      GitHub API URL: {{ default "https://api.github.com" $client.github.apiUrl | quote }}
      Repository Owner: {{ $client.repository.owner | quote }}
      Repository Name: {{ $client.repository.name | quote }}
      {{- with $client.repository.path }}
      Repository Path: {{ . | quote }}
      {{- end }}
      Default Branch: {{ default "main" $client.repository.branch | quote }}
      Authentication Type: {{ ternary "None" (ternary "GitHub App Installation" "Personal Access Token" (eq $client.github.auth.type "appInstallation")) (eq $client.github.auth.type "none") | quote }}
      Directory Filter Exclusion: {{ default "[.].*" $client.directoryFilterExclusion | quote }}
      Parameter Context Values: {{ ternary "Ignore Changes" (ternary "Remove" "Retain" (eq $client.parameterContextValues "remove")) (eq $client.parameterContextValues "ignoreChanges") | quote }}
      {{- with $client.sslContextServiceName }}
      SSL Context Service: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- if eq $client.provider "gitlab" }}
      GitLab API URL: {{ $client.gitlab.apiUrl | quote }}
      GitLab API Version: "V4"
      Repository Namespace: {{ $client.repository.namespace | quote }}
      Repository Name: {{ $client.repository.name | quote }}
      Authentication Type: "ACCESS_TOKEN"
      {{- with $client.repository.path }}
      Repository Path: {{ . | quote }}
      {{- end }}
      Default Branch: {{ default "main" $client.repository.branch | quote }}
      Directory Filter Exclusion: {{ default "[.].*" $client.directoryFilterExclusion | quote }}
      Parameter Context Values: {{ include "nifi.flowRegistryParameterContextValues" $client.parameterContextValues | trim | quote }}
      {{- with $client.sslContextServiceName }}
      SSL Context Service: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- if eq $client.provider "bitbucket" }}
      API URL: {{ $client.bitbucket.apiUrl | quote }}
      Bucket Name: {{ $client.repository.name | quote }}
      {{- if eq $client.bitbucket.formFactor "cloud" }}
      Workspace Name: {{ $client.repository.workspace | quote }}
      {{- end }}
      {{- if eq $client.bitbucket.formFactor "dataCenter" }}
      Project Key: {{ $client.repository.projectKey | quote }}
      {{- end }}
      {{- with $client.repository.path }}
      Repository Path: {{ . | quote }}
      {{- end }}
      Default Branch: {{ default "main" $client.repository.branch | quote }}
      Authentication Type: {{ ternary "OAuth 2 Access Token" (ternary "Username and Password" "Access Token" (eq $client.bitbucket.auth.type "basicAuth")) (eq $client.bitbucket.auth.type "oauth2") | quote }}
      Web Client Service: {{ $client.bitbucket.webClientServiceName | quote }}
      Directory Filter Exclusion: {{ default "[.].*" $client.directoryFilterExclusion | quote }}
      Parameter Context Values: {{ ternary "Ignore Changes" (ternary "Remove" "Retain" (eq $client.parameterContextValues "remove")) (eq $client.parameterContextValues "ignoreChanges") | quote }}
      {{- with $client.sslContextServiceName }}
      SSL Context Service: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- if eq $client.provider "azureDevOps" }}
      API URL: {{ $client.azureDevOps.apiUrl | quote }}
      Organization: {{ $client.azureDevOps.organization | quote }}
      Project: {{ $client.azureDevOps.project | quote }}
      Repository Name: {{ $client.repository.name | quote }}
      {{- with $client.repository.path }}
      Repository Path: {{ . | quote }}
      {{- end }}
      Default Branch: {{ default "main" $client.repository.branch | quote }}
      OAuth2 Access Token Provider: {{ $client.azureDevOps.oauth2AccessTokenProviderName | quote }}
      Web Client Service: {{ $client.azureDevOps.webClientServiceName | quote }}
      Directory Filter Exclusion: {{ default "[.].*" $client.directoryFilterExclusion | quote }}
      Parameter Context Values: {{ ternary "Ignore Changes" (ternary "Remove" "Retain" (eq $client.parameterContextValues "remove")) (eq $client.parameterContextValues "ignoreChanges") | quote }}
      {{- with $client.sslContextServiceName }}
      SSL Context Service: {{ . | quote }}
      {{- end }}
    {{- end }}
    {{- if or (eq $client.provider "github") (eq $client.provider "gitlab") (eq $client.provider "bitbucket") (eq $client.provider "azureDevOps") }}
    sensitivePropertyRefs:
    {{- if and (eq $client.provider "github") (or (not $client.github.auth.type) (eq $client.github.auth.type "personalAccessToken")) }}
      Personal Access Token:
        secretName: {{ $client.github.auth.personalAccessTokenSecret.name | quote }}
        secretKey: {{ $client.github.auth.personalAccessTokenSecret.key | quote }}
    {{- end }}
    {{- if and (eq $client.provider "github") (eq $client.github.auth.type "appInstallation") }}
      GitHub App ID:
        literalValue: {{ $client.github.auth.appId | quote }}
      GitHub App Installation ID:
        literalValue: {{ $client.github.auth.installationId | quote }}
      GitHub App Private Key:
        secretName: {{ $client.github.auth.privateKeySecret.name | quote }}
        secretKey: {{ $client.github.auth.privateKeySecret.key | quote }}
    {{- end }}
    {{- if eq $client.provider "gitlab" }}
      Access Token:
        secretName: {{ $client.gitlab.accessTokenSecret.name | quote }}
        secretKey: {{ $client.gitlab.accessTokenSecret.key | quote }}
    {{- end }}
    {{- if and (eq $client.provider "bitbucket") (eq $client.bitbucket.auth.type "accessToken") }}
      Access Token:
        secretName: {{ $client.bitbucket.auth.accessTokenSecret.name | quote }}
        secretKey: {{ $client.bitbucket.auth.accessTokenSecret.key | quote }}
    {{- end }}
    {{- if and (eq $client.provider "bitbucket") (eq $client.bitbucket.auth.type "basicAuth") }}
      Username:
        literalValue: {{ $client.bitbucket.auth.username | quote }}
      Password:
        secretName: {{ $client.bitbucket.auth.passwordSecret.name | quote }}
        secretKey: {{ $client.bitbucket.auth.passwordSecret.key | quote }}
    {{- end }}
    {{- if and (eq $client.provider "bitbucket") (eq $client.bitbucket.auth.type "oauth2") }}
      OAuth2 Access Token Provider:
        controllerServiceName: {{ $client.bitbucket.auth.oauth2AccessTokenProviderName | quote }}
    {{- end }}
    {{- if eq $client.provider "azureDevOps" }}
      OAuth2 Access Token Provider:
        controllerServiceName: {{ $client.azureDevOps.oauth2AccessTokenProviderName | quote }}
    {{- end }}
    {{- end }}
{{- end }}
{{- end -}}
