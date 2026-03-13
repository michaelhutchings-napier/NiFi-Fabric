{{- define "nifi.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "nifi.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "nifi.labels" -}}
app.kubernetes.io/name: {{ include "nifi.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "nifi.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nifi.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "nifi.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "nifi.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "nifi.proxyHosts" -}}
{{- $root := . -}}
{{- $hosts := list -}}
{{- $fullname := include "nifi.fullname" $root -}}
{{- range $ordinal := until (int $root.Values.replicaCount) -}}
{{- $hosts = append $hosts (printf "%s-%d.%s-headless.%s.svc.cluster.local:%v" $fullname $ordinal $fullname $root.Release.Namespace $root.Values.ports.https) -}}
{{- end -}}
{{- $hosts = append $hosts (printf "%s.%s.svc.cluster.local:%v" $fullname $root.Release.Namespace $root.Values.ports.https) -}}
{{- range $host := $root.Values.web.proxyHosts -}}
{{- $hosts = append $hosts $host -}}
{{- end -}}
{{- join "," (uniq $hosts) -}}
{{- end -}}

{{- define "nifi.tlsMode" -}}
{{- $mode := default "externalSecret" .Values.tls.mode -}}
{{- if and .Values.tls.certManager.enabled (ne $mode "certManager") -}}
{{- fail "tls.certManager.enabled=true requires tls.mode=certManager" -}}
{{- end -}}
{{- if and (eq $mode "certManager") (not .Values.tls.certManager.enabled) -}}
{{- fail "tls.mode=certManager requires tls.certManager.enabled=true" -}}
{{- end -}}
{{- if and (ne $mode "externalSecret") (ne $mode "certManager") -}}
{{- fail "tls.mode must be one of: externalSecret, certManager" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "nifi.tlsSecretName" -}}
{{- if eq (include "nifi.tlsMode" .) "certManager" -}}
{{- required "tls.certManager.secretName is required when tls.mode=certManager" .Values.tls.certManager.secretName -}}
{{- else -}}
{{- required "tls.existingSecret is required when tls.mode=externalSecret" .Values.tls.existingSecret -}}
{{- end -}}
{{- end -}}

{{- define "nifi.sensitivePropsSecretName" -}}
{{- if .Values.tls.sensitiveProps.secretRef.name -}}
{{- .Values.tls.sensitiveProps.secretRef.name -}}
{{- else if eq (include "nifi.tlsMode" .) "externalSecret" -}}
{{- include "nifi.tlsSecretName" . -}}
{{- else -}}
{{- fail "tls.sensitiveProps.secretRef.name or tls.sensitiveProps.value is required when tls.mode=certManager" -}}
{{- end -}}
{{- end -}}

{{- define "nifi.sensitivePropsSecretKey" -}}
{{- default .Values.tls.sensitivePropsKeyKey .Values.tls.sensitiveProps.secretRef.key -}}
{{- end -}}

{{- define "nifi.certManagerPKCS12PasswordSecretName" -}}
{{- if .Values.tls.certManager.pkcs12.passwordSecretRef.name -}}
{{- .Values.tls.certManager.pkcs12.passwordSecretRef.name -}}
{{- else -}}
{{- fail "tls.certManager.pkcs12.passwordSecretRef.name is required when tls.mode=certManager and tls.certManager.pkcs12.password is empty" -}}
{{- end -}}
{{- end -}}

{{- define "nifi.certManagerPKCS12PasswordSecretKey" -}}
{{- if .Values.tls.certManager.pkcs12.passwordSecretRef.key -}}
{{- .Values.tls.certManager.pkcs12.passwordSecretRef.key -}}
{{- else -}}
{{- fail "tls.certManager.pkcs12.passwordSecretRef.key is required when tls.mode=certManager and tls.certManager.pkcs12.password is empty" -}}
{{- end -}}
{{- end -}}

{{- define "nifi.certManagerCommonName" -}}
{{- if .Values.tls.certManager.commonName -}}
{{- .Values.tls.certManager.commonName -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local" (include "nifi.fullname" .) .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{- define "nifi.metricsMode" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $mode := default "disabled" $metrics.mode -}}
{{- if and (eq $mode "disabled") .Values.serviceMonitor.enabled -}}
nativeApiLegacy
{{- else -}}
{{- $mode -}}
{{- end -}}
{{- end -}}

{{- define "nifi.metricsServiceName" -}}
{{- printf "%s-metrics" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.metricsServiceLabels" -}}
{{- include "nifi.labels" . }}
app.kubernetes.io/component: metrics
{{- end -}}

{{- define "nifi.metricsExporterName" -}}
{{- printf "%s-metrics-exporter" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.metricsExporterLabels" -}}
{{- include "nifi.labels" . }}
app.kubernetes.io/component: metrics-exporter
{{- end -}}

{{- define "nifi.metricsExporterSelectorLabels" -}}
{{- include "nifi.selectorLabels" . }}
app.kubernetes.io/component: metrics-exporter
{{- end -}}

{{- define "nifi.metricsServiceSelectorLabels" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $service := default (dict) $native.service -}}
{{- $mode := default "disabled" $metrics.mode -}}
{{- if eq $mode "exporter" -}}
{{- include "nifi.metricsExporterSelectorLabels" . }}
{{- else if $service.enabled -}}
{{- include "nifi.selectorLabels" . }}
app.kubernetes.io/component: metrics
{{- else -}}
{{- include "nifi.selectorLabels" . }}
{{- end -}}
{{- end -}}

{{- define "nifi.metricsEndpointName" -}}
{{- $raw := . -}}
{{- $clean := regexReplaceAll "[^a-z0-9-]+" (lower $raw) "-" -}}
{{- $clean = regexReplaceAll "-+" $clean "-" -}}
{{- trimAll "-" $clean -}}
{{- end -}}

{{- define "nifi.metricsTLSServerName" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $tlsConfig := default (dict) $native.tlsConfig -}}
{{- if $tlsConfig.serverName -}}
{{- $tlsConfig.serverName -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local" (include "nifi.fullname" .) .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{- define "nifi.exporterSourceHost" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $source := default (dict) $exporter.source -}}
{{- if $source.host -}}
{{- $source.host -}}
{{- else -}}
{{- printf "%s.%s.svc.cluster.local" (include "nifi.fullname" .) .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{- define "nifi.validate" -}}
{{- $mode := include "nifi.metricsMode" . -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $configuredMode := default "disabled" $metrics.mode -}}
{{- if and (ne $mode "disabled") (ne $mode "nativeApi") (ne $mode "nativeApiLegacy") (ne $mode "exporter") (ne $mode "siteToSite") -}}
{{- fail "observability.metrics.mode must be one of: disabled, nativeApi, exporter, siteToSite" -}}
{{- end -}}
{{- if and .Values.serviceMonitor.enabled (ne $configuredMode "disabled") -}}
{{- fail "serviceMonitor.enabled is deprecated and cannot be combined with observability.metrics.mode; use observability.metrics only" -}}
{{- end -}}
{{- if eq $mode "exporter" -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $machineAuth := default (dict) $exporter.machineAuth -}}
{{- $source := default (dict) $exporter.source -}}
{{- $service := default (dict) $exporter.service -}}
{{- $serviceMonitor := default (dict) $exporter.serviceMonitor -}}
{{- if and (ne $machineAuth.type "bearerToken") (ne $machineAuth.type "authorizationHeader") -}}
{{- fail "observability.metrics.exporter.machineAuth.type must be one of: bearerToken, authorizationHeader" -}}
{{- end -}}
{{- if not $machineAuth.secretRef.name -}}
{{- fail "observability.metrics.exporter.machineAuth.secretRef.name is required when observability.metrics.mode=exporter" -}}
{{- end -}}
{{- if eq $machineAuth.type "bearerToken" -}}
{{- if not $machineAuth.bearerToken.tokenKey -}}
{{- fail "observability.metrics.exporter.machineAuth.bearerToken.tokenKey is required for bearerToken" -}}
{{- end -}}
{{- end -}}
{{- if eq $machineAuth.type "authorizationHeader" -}}
{{- if not $machineAuth.authorization.type -}}
{{- fail "observability.metrics.exporter.machineAuth.authorization.type is required for authorizationHeader" -}}
{{- end -}}
{{- if not $machineAuth.authorization.credentialsKey -}}
{{- fail "observability.metrics.exporter.machineAuth.authorization.credentialsKey is required for authorizationHeader" -}}
{{- end -}}
{{- end -}}
{{- if and (ne $source.scheme "http") (ne $source.scheme "https") -}}
{{- fail "observability.metrics.exporter.source.scheme must be one of: http, https" -}}
{{- end -}}
{{- if not $source.path -}}
{{- fail "observability.metrics.exporter.source.path is required when observability.metrics.mode=exporter" -}}
{{- end -}}
{{- if and (not $service.enabled) (default true $serviceMonitor.enabled) -}}
{{- fail "observability.metrics.exporter.service.enabled=false cannot be combined with an enabled exporter ServiceMonitor" -}}
{{- end -}}
{{- $sourceTLSConfig := default (dict) $source.tlsConfig -}}
{{- $sourceTLSCA := default (dict) $sourceTLSConfig.ca -}}
{{- $sourceTLSCASecretRef := default (dict) $sourceTLSCA.secretRef -}}
{{- if and $sourceTLSCASecretRef.name (not $sourceTLSCASecretRef.key) -}}
{{- fail "observability.metrics.exporter.source.tlsConfig.ca.secretRef.key is required when a CA Secret reference is configured" -}}
{{- end -}}
{{- end -}}
{{- if eq $mode "siteToSite" -}}
{{- fail "observability.metrics.mode=siteToSite is prepared-only in this slice; the chart defines observability.metrics.siteToSite.* for a future SiteToSiteMetricsReportingTask path but does not yet manage NiFi reporting tasks or the destination receiver pipeline. Use nativeApi or exporter." -}}
{{- end -}}
{{- if eq $mode "nativeApi" -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- if not $native.endpoints -}}
{{- fail "observability.metrics.nativeApi.endpoints must contain at least one endpoint when observability.metrics.mode=nativeApi" -}}
{{- end -}}
{{- $enabledCount := 0 -}}
{{- range $endpoint := $native.endpoints -}}
{{- if $endpoint.enabled -}}
{{- $enabledCount = add $enabledCount 1 -}}
{{- if not $endpoint.name -}}
{{- fail "observability.metrics.nativeApi.endpoints[].name is required for enabled endpoints" -}}
{{- end -}}
{{- if not $endpoint.path -}}
{{- fail (printf "observability.metrics.nativeApi.endpoints[%s].path is required" $endpoint.name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if lt $enabledCount 1 -}}
{{- fail "observability.metrics.nativeApi requires at least one enabled endpoint" -}}
{{- end -}}
{{- $machineAuth := default (dict) $native.machineAuth -}}
{{- if and (ne $machineAuth.type "") (ne $machineAuth.type "none") (ne $machineAuth.type "basicAuth") (ne $machineAuth.type "bearerToken") (ne $machineAuth.type "authorizationHeader") -}}
{{- fail "observability.metrics.nativeApi.machineAuth.type must be one of: none, basicAuth, bearerToken, authorizationHeader" -}}
{{- end -}}
{{- if and (ne $machineAuth.type "") (ne $machineAuth.type "none") (not $machineAuth.secretRef.name) -}}
{{- fail "observability.metrics.nativeApi.machineAuth.secretRef.name is required when machine auth is enabled" -}}
{{- end -}}
{{- if eq $machineAuth.type "basicAuth" -}}
{{- if not $machineAuth.basicAuth.usernameKey -}}
{{- fail "observability.metrics.nativeApi.machineAuth.basicAuth.usernameKey is required for basicAuth" -}}
{{- end -}}
{{- if not $machineAuth.basicAuth.passwordKey -}}
{{- fail "observability.metrics.nativeApi.machineAuth.basicAuth.passwordKey is required for basicAuth" -}}
{{- end -}}
{{- end -}}
{{- if eq $machineAuth.type "bearerToken" -}}
{{- if not $machineAuth.bearerToken.tokenKey -}}
{{- fail "observability.metrics.nativeApi.machineAuth.bearerToken.tokenKey is required for bearerToken" -}}
{{- end -}}
{{- end -}}
{{- if eq $machineAuth.type "authorizationHeader" -}}
{{- if not $machineAuth.authorization.type -}}
{{- fail "observability.metrics.nativeApi.machineAuth.authorization.type is required for authorizationHeader" -}}
{{- end -}}
{{- if not $machineAuth.authorization.credentialsKey -}}
{{- fail "observability.metrics.nativeApi.machineAuth.authorization.credentialsKey is required for authorizationHeader" -}}
{{- end -}}
{{- end -}}
{{- $tlsConfig := default (dict) $native.tlsConfig -}}
{{- $tlsCA := default (dict) $tlsConfig.ca -}}
{{- $tlsCASecretRef := default (dict) $tlsCA.secretRef -}}
{{- if and $tlsCASecretRef.name (not $tlsCASecretRef.key) -}}
{{- fail "observability.metrics.nativeApi.tlsConfig.ca.secretRef.key is required when a CA Secret reference is configured" -}}
{{- end -}}
{{- end -}}
{{- end -}}
