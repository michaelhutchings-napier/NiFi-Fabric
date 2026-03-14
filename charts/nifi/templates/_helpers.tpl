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

{{- define "nifi.trustManagerBundleName" -}}
{{- $ref := default (dict) .Values.trustManagerBundleRef -}}
{{- if $ref.name -}}
{{- $ref.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-trust-bundle" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "nifi.trustManagerBundleRefType" -}}
{{- $ref := default (dict) .Values.trustManagerBundleRef -}}
{{- default "configMap" $ref.type -}}
{{- end -}}

{{- define "nifi.trustManagerBundleRefKey" -}}
{{- $ref := default (dict) .Values.trustManagerBundleRef -}}
{{- default "ca.crt" $ref.key -}}
{{- end -}}

{{- define "nifi.additionalTrustBundleRefType" -}}
{{- $bundle := default (dict) .Values.tls.additionalTrustBundle -}}
{{- if $bundle.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefType" . -}}
{{- else if $bundle.configMapRef.name -}}
configMap
{{- else if $bundle.secretRef.name -}}
secret
{{- end -}}
{{- end -}}

{{- define "nifi.additionalTrustBundleRefName" -}}
{{- $bundle := default (dict) .Values.tls.additionalTrustBundle -}}
{{- if $bundle.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleName" . -}}
{{- else if $bundle.configMapRef.name -}}
{{- $bundle.configMapRef.name -}}
{{- else if $bundle.secretRef.name -}}
{{- $bundle.secretRef.name -}}
{{- end -}}
{{- end -}}

{{- define "nifi.additionalTrustBundleRefKey" -}}
{{- $bundle := default (dict) .Values.tls.additionalTrustBundle -}}
{{- if $bundle.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefKey" . -}}
{{- else if $bundle.configMapRef.name -}}
{{- $bundle.configMapRef.key -}}
{{- else if $bundle.secretRef.name -}}
{{- $bundle.secretRef.key -}}
{{- end -}}
{{- end -}}

{{- define "nifi.nativeMetricsCARefType" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $tlsConfig := default (dict) $native.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefType" . -}}
{{- else if $ca.configMapRef.name -}}
configMap
{{- else if $ca.secretRef.name -}}
secret
{{- end -}}
{{- end -}}

{{- define "nifi.nativeMetricsCARefName" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $tlsConfig := default (dict) $native.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleName" . -}}
{{- else if $ca.configMapRef.name -}}
{{- $ca.configMapRef.name -}}
{{- else if $ca.secretRef.name -}}
{{- $ca.secretRef.name -}}
{{- end -}}
{{- end -}}

{{- define "nifi.nativeMetricsCARefKey" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $tlsConfig := default (dict) $native.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefKey" . -}}
{{- else if $ca.configMapRef.name -}}
{{- $ca.configMapRef.key -}}
{{- else if $ca.secretRef.name -}}
{{- $ca.secretRef.key -}}
{{- end -}}
{{- end -}}

{{- define "nifi.exporterSourceCARefType" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $source := default (dict) $exporter.source -}}
{{- $tlsConfig := default (dict) $source.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefType" . -}}
{{- else if $ca.configMapRef.name -}}
configMap
{{- else if $ca.secretRef.name -}}
secret
{{- end -}}
{{- end -}}

{{- define "nifi.exporterSourceCARefName" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $source := default (dict) $exporter.source -}}
{{- $tlsConfig := default (dict) $source.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleName" . -}}
{{- else if $ca.configMapRef.name -}}
{{- $ca.configMapRef.name -}}
{{- else if $ca.secretRef.name -}}
{{- $ca.secretRef.name -}}
{{- end -}}
{{- end -}}

{{- define "nifi.exporterSourceCARefKey" -}}
{{- $observability := default (dict) .Values.observability -}}
{{- $metrics := default (dict) $observability.metrics -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $source := default (dict) $exporter.source -}}
{{- $tlsConfig := default (dict) $source.tlsConfig -}}
{{- $ca := default (dict) $tlsConfig.ca -}}
{{- if $ca.useTrustManagerBundle -}}
{{- include "nifi.trustManagerBundleRefKey" . -}}
{{- else if $ca.configMapRef.name -}}
{{- $ca.configMapRef.key -}}
{{- else if $ca.secretRef.name -}}
{{- $ca.secretRef.key -}}
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
{{- $additionalTrustBundle := default (dict) .Values.tls.additionalTrustBundle -}}
{{- $additionalTrustConfigMapRef := default (dict) $additionalTrustBundle.configMapRef -}}
{{- $additionalTrustSecretRef := default (dict) $additionalTrustBundle.secretRef -}}
{{- $trustManagerBundleRef := default (dict) .Values.trustManagerBundleRef -}}
{{- $nativeMetrics := default (dict) $metrics.nativeApi -}}
{{- $nativeTLSConfig := default (dict) $nativeMetrics.tlsConfig -}}
{{- $nativeTLSCA := default (dict) $nativeTLSConfig.ca -}}
{{- $exporterMetrics := default (dict) $metrics.exporter -}}
{{- $exporterSourceConfig := default (dict) $exporterMetrics.source -}}
{{- $exporterSourceTLSConfig := default (dict) $exporterSourceConfig.tlsConfig -}}
{{- $exporterSourceTLSCA := default (dict) $exporterSourceTLSConfig.ca -}}
{{- if or $additionalTrustBundle.useTrustManagerBundle $nativeTLSCA.useTrustManagerBundle $exporterSourceTLSCA.useTrustManagerBundle -}}
{{- $trustManagerBundleRefType := default "configMap" $trustManagerBundleRef.type -}}
{{- if and (ne $trustManagerBundleRefType "configMap") (ne $trustManagerBundleRefType "secret") -}}
{{- fail "trustManagerBundleRef.type must be one of: configMap, secret" -}}
{{- end -}}
{{- if not (default "ca.crt" $trustManagerBundleRef.key) -}}
{{- fail "trustManagerBundleRef.key is required when useTrustManagerBundle=true" -}}
{{- end -}}
{{- end -}}
{{- if $additionalTrustBundle.enabled -}}
{{- if and $additionalTrustBundle.useTrustManagerBundle (or $additionalTrustConfigMapRef.name $additionalTrustSecretRef.name) -}}
{{- fail "tls.additionalTrustBundle.useTrustManagerBundle cannot be combined with configMapRef or secretRef" -}}
{{- end -}}
{{- if and $additionalTrustConfigMapRef.name $additionalTrustSecretRef.name -}}
{{- fail "tls.additionalTrustBundle supports either configMapRef or secretRef, not both" -}}
{{- end -}}
{{- if and (not $additionalTrustBundle.useTrustManagerBundle) (not $additionalTrustConfigMapRef.name) (not $additionalTrustSecretRef.name) -}}
{{- fail "tls.additionalTrustBundle requires useTrustManagerBundle=true or a configMapRef/secretRef" -}}
{{- end -}}
{{- if and $additionalTrustConfigMapRef.name (not $additionalTrustConfigMapRef.key) -}}
{{- fail "tls.additionalTrustBundle.configMapRef.key is required when a ConfigMap reference is configured" -}}
{{- end -}}
{{- if and $additionalTrustSecretRef.name (not $additionalTrustSecretRef.key) -}}
{{- fail "tls.additionalTrustBundle.secretRef.key is required when a Secret reference is configured" -}}
{{- end -}}
{{- end -}}
{{- if eq $mode "exporter" -}}
{{- $exporter := default (dict) $metrics.exporter -}}
{{- $machineAuth := default (dict) $exporter.machineAuth -}}
{{- $source := default (dict) $exporter.source -}}
{{- $supplemental := default (dict) $exporter.supplemental -}}
{{- $flowStatus := default (dict) $supplemental.flowStatus -}}
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
{{- if le (int $source.timeoutSeconds) 0 -}}
{{- fail "observability.metrics.exporter.source.timeoutSeconds must be greater than zero" -}}
{{- end -}}
{{- if not $source.path -}}
{{- fail "observability.metrics.exporter.source.path is required when observability.metrics.mode=exporter" -}}
{{- end -}}
{{- if and (not $service.enabled) (default true $serviceMonitor.enabled) -}}
{{- fail "observability.metrics.exporter.service.enabled=false cannot be combined with an enabled exporter ServiceMonitor" -}}
{{- end -}}
{{- $serviceMonitorDefaults := default (dict) $serviceMonitor.defaults -}}
{{- $serviceMonitorScheme := default "http" $serviceMonitorDefaults.scheme -}}
{{- if and (ne $serviceMonitorScheme "http") (ne $serviceMonitorScheme "https") -}}
{{- fail "observability.metrics.exporter.serviceMonitor.defaults.scheme must be one of: http, https" -}}
{{- end -}}
{{- if and $flowStatus.enabled (not $flowStatus.path) -}}
{{- fail "observability.metrics.exporter.supplemental.flowStatus.path is required when flowStatus metrics are enabled" -}}
{{- end -}}
{{- $sourceTLSConfig := default (dict) $source.tlsConfig -}}
{{- $sourceTLSCA := default (dict) $sourceTLSConfig.ca -}}
{{- $sourceTLSCAConfigMapRef := default (dict) $sourceTLSCA.configMapRef -}}
{{- $sourceTLSCASecretRef := default (dict) $sourceTLSCA.secretRef -}}
{{- if and $sourceTLSCA.useTrustManagerBundle (or $sourceTLSCAConfigMapRef.name $sourceTLSCASecretRef.name) -}}
{{- fail "observability.metrics.exporter.source.tlsConfig.ca.useTrustManagerBundle cannot be combined with configMapRef or secretRef" -}}
{{- end -}}
{{- if and $sourceTLSCAConfigMapRef.name $sourceTLSCASecretRef.name -}}
{{- fail "observability.metrics.exporter.source.tlsConfig.ca supports either configMapRef or secretRef, not both" -}}
{{- end -}}
{{- if and $sourceTLSCAConfigMapRef.name (not $sourceTLSCAConfigMapRef.key) -}}
{{- fail "observability.metrics.exporter.source.tlsConfig.ca.configMapRef.key is required when a CA ConfigMap reference is configured" -}}
{{- end -}}
{{- if and $sourceTLSCASecretRef.name (not $sourceTLSCASecretRef.key) -}}
{{- fail "observability.metrics.exporter.source.tlsConfig.ca.secretRef.key is required when a CA Secret reference is configured" -}}
{{- end -}}
{{- end -}}
{{- if eq $mode "siteToSite" -}}
{{- $siteToSite := default (dict) $metrics.siteToSite -}}
{{- $destination := default (dict) $siteToSite.destination -}}
{{- $destinationAuth := default (dict) $destination.auth -}}
{{- $destinationTLS := default (dict) $destination.tls -}}
{{- $destinationTLSCA := default (dict) $destinationTLS.ca -}}
{{- $destinationTLSCASecretRef := default (dict) $destinationTLSCA.secretRef -}}
{{- $source := default (dict) $siteToSite.source -}}
{{- $transport := default (dict) $siteToSite.transport -}}
{{- $format := default (dict) $siteToSite.format -}}
{{- if not $destination.url -}}
{{- fail "observability.metrics.siteToSite.destination.url is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if not (or (hasPrefix "https://" $destination.url) (hasPrefix "http://" $destination.url)) -}}
{{- fail "observability.metrics.siteToSite.destination.url must start with http:// or https://" -}}
{{- end -}}
{{- if not $destination.inputPortName -}}
{{- fail "observability.metrics.siteToSite.destination.inputPortName is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if and (ne $destinationAuth.type "") (ne $destinationAuth.type "none") (ne $destinationAuth.type "bearerToken") (ne $destinationAuth.type "authorizationHeader") -}}
{{- fail "observability.metrics.siteToSite.destination.auth.type must be one of: none, bearerToken, authorizationHeader" -}}
{{- end -}}
{{- if and (ne $destinationAuth.type "") (ne $destinationAuth.type "none") (not $destinationAuth.secretRef.name) -}}
{{- fail "observability.metrics.siteToSite.destination.auth.secretRef.name is required when destination auth is enabled" -}}
{{- end -}}
{{- if eq $destinationAuth.type "bearerToken" -}}
{{- if not $destinationAuth.bearerToken.tokenKey -}}
{{- fail "observability.metrics.siteToSite.destination.auth.bearerToken.tokenKey is required for bearerToken" -}}
{{- end -}}
{{- end -}}
{{- if eq $destinationAuth.type "authorizationHeader" -}}
{{- if not $destinationAuth.authorization.type -}}
{{- fail "observability.metrics.siteToSite.destination.auth.authorization.type is required for authorizationHeader" -}}
{{- end -}}
{{- if not $destinationAuth.authorization.credentialsKey -}}
{{- fail "observability.metrics.siteToSite.destination.auth.authorization.credentialsKey is required for authorizationHeader" -}}
{{- end -}}
{{- end -}}
{{- if and $destinationTLSCASecretRef.name (not $destinationTLSCASecretRef.key) -}}
{{- fail "observability.metrics.siteToSite.destination.tls.ca.secretRef.key is required when a CA Secret reference is configured" -}}
{{- end -}}
{{- if and (hasPrefix "http://" $destination.url) (or $destinationTLS.insecureSkipVerify $destinationTLSCASecretRef.name) -}}
{{- fail "observability.metrics.siteToSite.destination.tls.* cannot be set for an http:// destination.url" -}}
{{- end -}}
{{- if and (ne $transport.protocol "RAW") (ne $transport.protocol "HTTP") -}}
{{- fail "observability.metrics.siteToSite.transport.protocol must be one of: RAW, HTTP" -}}
{{- end -}}
{{- if not $transport.communicationsTimeout -}}
{{- fail "observability.metrics.siteToSite.transport.communicationsTimeout is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if and $source.instanceUrl (not (or (hasPrefix "https://" $source.instanceUrl) (hasPrefix "http://" $source.instanceUrl))) -}}
{{- fail "observability.metrics.siteToSite.source.instanceUrl must start with http:// or https://" -}}
{{- end -}}
{{- if ne $format.type "AmbariFormat" -}}
{{- fail "observability.metrics.siteToSite.format.type must be AmbariFormat in the current prepared contract" -}}
{{- end -}}
{{- fail "observability.metrics.mode=siteToSite remains prepared-only. The chart validates the destination, auth, TLS, transport, and format contract, but this repo still does not own NiFi reporting-task lifecycle or the destination receiver/input-port pipeline. Use nativeApi or exporter for runtime support." -}}
{{- end -}}
{{- if eq $mode "nativeApi" -}}
{{- $native := default (dict) $metrics.nativeApi -}}
{{- $defaults := default (dict) $native.serviceMonitor.defaults -}}
{{- if not $native.endpoints -}}
{{- fail "observability.metrics.nativeApi.endpoints must contain at least one endpoint when observability.metrics.mode=nativeApi" -}}
{{- end -}}
{{- $enabledCount := 0 -}}
{{- $defaultScheme := default "https" $defaults.scheme -}}
{{- if and (ne $defaultScheme "http") (ne $defaultScheme "https") -}}
{{- fail "observability.metrics.nativeApi.serviceMonitor.defaults.scheme must be one of: http, https" -}}
{{- end -}}
{{- $endpointNames := dict -}}
{{- range $endpoint := $native.endpoints -}}
{{- if $endpoint.enabled -}}
{{- $enabledCount = add $enabledCount 1 -}}
{{- if not $endpoint.name -}}
{{- fail "observability.metrics.nativeApi.endpoints[].name is required for enabled endpoints" -}}
{{- end -}}
{{- $normalizedName := include "nifi.metricsEndpointName" $endpoint.name -}}
{{- if not $normalizedName -}}
{{- fail (printf "observability.metrics.nativeApi.endpoints[%s].name must contain at least one alphanumeric character" $endpoint.name) -}}
{{- end -}}
{{- if hasKey $endpointNames $normalizedName -}}
{{- fail (printf "observability.metrics.nativeApi.endpoints[%s].name collides with another endpoint after Kubernetes name sanitizing" $endpoint.name) -}}
{{- end -}}
{{- $_ := set $endpointNames $normalizedName true -}}
{{- if not $endpoint.path -}}
{{- fail (printf "observability.metrics.nativeApi.endpoints[%s].path is required" $endpoint.name) -}}
{{- end -}}
{{- $endpointScheme := default $defaultScheme $endpoint.scheme -}}
{{- if and (ne $endpointScheme "http") (ne $endpointScheme "https") -}}
{{- fail (printf "observability.metrics.nativeApi.endpoints[%s].scheme must be one of: http, https" $endpoint.name) -}}
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
{{- $tlsCAConfigMapRef := default (dict) $tlsCA.configMapRef -}}
{{- $tlsCASecretRef := default (dict) $tlsCA.secretRef -}}
{{- if and $tlsCA.useTrustManagerBundle (or $tlsCAConfigMapRef.name $tlsCASecretRef.name) -}}
{{- fail "observability.metrics.nativeApi.tlsConfig.ca.useTrustManagerBundle cannot be combined with configMapRef or secretRef" -}}
{{- end -}}
{{- if and $tlsCAConfigMapRef.name $tlsCASecretRef.name -}}
{{- fail "observability.metrics.nativeApi.tlsConfig.ca supports either configMapRef or secretRef, not both" -}}
{{- end -}}
{{- if and $tlsCAConfigMapRef.name (not $tlsCAConfigMapRef.key) -}}
{{- fail "observability.metrics.nativeApi.tlsConfig.ca.configMapRef.key is required when a CA ConfigMap reference is configured" -}}
{{- end -}}
{{- if and $tlsCASecretRef.name (not $tlsCASecretRef.key) -}}
{{- fail "observability.metrics.nativeApi.tlsConfig.ca.secretRef.key is required when a CA Secret reference is configured" -}}
{{- end -}}
{{- end -}}
{{- end -}}
