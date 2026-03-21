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

{{- define "nifi.linkerdOpaquePortsCSV" -}}
{{- $linkerd := default (dict) .Values.linkerd -}}
{{- if not $linkerd.enabled -}}
{{- "" -}}
{{- else -}}
{{- $opaque := default (dict) $linkerd.opaquePorts -}}
{{- $clusterOpaque := true -}}
{{- if hasKey $opaque "cluster" -}}
{{- $clusterOpaque = $opaque.cluster -}}
{{- end -}}
{{- $loadBalanceOpaque := true -}}
{{- if hasKey $opaque "loadBalance" -}}
{{- $loadBalanceOpaque = $opaque.loadBalance -}}
{{- end -}}
{{- $httpsOpaque := false -}}
{{- if hasKey $opaque "https" -}}
{{- $httpsOpaque = $opaque.https -}}
{{- end -}}
{{- $ports := list -}}
{{- if $clusterOpaque -}}
{{- $ports = append $ports (printf "%v" .Values.ports.cluster) -}}
{{- end -}}
{{- if $loadBalanceOpaque -}}
{{- $ports = append $ports (printf "%v" .Values.ports.loadBalance) -}}
{{- end -}}
{{- if $httpsOpaque -}}
{{- $ports = append $ports (printf "%v" .Values.ports.https) -}}
{{- end -}}
{{- range $port := default (list) $opaque.additional -}}
{{- $ports = append $ports (printf "%v" $port) -}}
{{- end -}}
{{- join "," (uniq $ports) -}}
{{- end -}}
{{- end -}}

{{- define "nifi.linkerdOpaquePortsForService" -}}
{{- $opaqueCSV := include "nifi.linkerdOpaquePortsCSV" .root -}}
{{- if not $opaqueCSV -}}
{{- "" -}}
{{- else -}}
{{- $configured := splitList "," $opaqueCSV -}}
{{- $matches := list -}}
{{- range $port := .servicePorts -}}
{{- $portString := printf "%v" $port -}}
{{- if has $portString $configured -}}
{{- $matches = append $matches $portString -}}
{{- end -}}
{{- end -}}
{{- join "," (uniq $matches) -}}
{{- end -}}
{{- end -}}

{{- define "nifi.linkerdPodAnnotations" -}}
{{- $annotations := dict -}}
{{- $linkerd := default (dict) .Values.linkerd -}}
{{- if $linkerd.enabled -}}
{{- $_ := set $annotations "linkerd.io/inject" (default "enabled" $linkerd.inject) -}}
{{- $opaquePorts := include "nifi.linkerdOpaquePortsCSV" . -}}
{{- if $opaquePorts -}}
{{- $_ := set $annotations "config.linkerd.io/opaque-ports" $opaquePorts -}}
{{- end -}}
{{- end -}}
{{- toYaml $annotations -}}
{{- end -}}

{{- define "nifi.linkerdServiceAnnotations" -}}
{{- $annotations := dict -}}
{{- $linkerd := default (dict) .root.Values.linkerd -}}
{{- if $linkerd.enabled -}}
{{- $opaquePorts := include "nifi.linkerdOpaquePortsForService" . -}}
{{- if $opaquePorts -}}
{{- $_ := set $annotations "config.linkerd.io/opaque-ports" $opaquePorts -}}
{{- end -}}
{{- end -}}
{{- toYaml $annotations -}}
{{- end -}}

{{- define "nifi.istioPodAnnotations" -}}
{{- $annotations := dict -}}
{{- $istio := default (dict) .Values.istio -}}
{{- if $istio.enabled -}}
{{- $inject := true -}}
{{- if hasKey $istio "inject" -}}
{{- $inject = $istio.inject -}}
{{- end -}}
{{- $rewriteAppHTTPProbers := true -}}
{{- if hasKey $istio "rewriteAppHTTPProbers" -}}
{{- $rewriteAppHTTPProbers = $istio.rewriteAppHTTPProbers -}}
{{- end -}}
{{- $holdApplicationUntilProxyStarts := true -}}
{{- if hasKey $istio "holdApplicationUntilProxyStarts" -}}
{{- $holdApplicationUntilProxyStarts = $istio.holdApplicationUntilProxyStarts -}}
{{- end -}}
{{- $_ := set $annotations "sidecar.istio.io/inject" (ternary "true" "false" $inject) -}}
{{- $_ := set $annotations "sidecar.istio.io/rewriteAppHTTPProbers" (ternary "true" "false" $rewriteAppHTTPProbers) -}}
{{- if $holdApplicationUntilProxyStarts -}}
{{- $_ := set $annotations "proxy.istio.io/config" ((dict "holdApplicationUntilProxyStarts" true) | toJson) -}}
{{- end -}}
{{- range $key, $value := (default (dict) $istio.annotations) -}}
{{- $_ := set $annotations $key $value -}}
{{- end -}}
{{- end -}}
{{- toYaml $annotations -}}
{{- end -}}

{{- define "nifi.ambientPodLabels" -}}
{{- $labels := dict -}}
{{- $ambient := default (dict) .Values.ambient -}}
{{- if $ambient.enabled -}}
{{- $_ := set $labels "istio.io/dataplane-mode" (default "ambient" $ambient.dataplaneMode) -}}
{{- range $key, $value := (default (dict) $ambient.labels) -}}
{{- $_ := set $labels $key $value -}}
{{- end -}}
{{- end -}}
{{- toYaml $labels -}}
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

{{- define "nifi.siteToSiteMetricsConfigName" -}}
{{- printf "%s-site-to-site-metrics" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.siteToSiteStatusConfigName" -}}
{{- printf "%s-site-to-site-status" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.siteToSiteProvenanceConfigName" -}}
{{- printf "%s-site-to-site-provenance" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.parameterContextsConfigName" -}}
{{- printf "%s-parameter-contexts" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi.versionedFlowImportsConfigName" -}}
{{- printf "%s-versioned-flow-imports" (include "nifi.fullname" .) | trunc 63 | trimSuffix "-" -}}
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
{{- $linkerd := default (dict) .Values.linkerd -}}
{{- $istio := default (dict) .Values.istio -}}
{{- $ambient := default (dict) .Values.ambient -}}
{{- $route := default (dict) .Values.openshift.route -}}
{{- $siteToSiteMetrics := default (dict) $metrics.siteToSite -}}
{{- $siteToSiteStatus := default (dict) $observability.siteToSiteStatus -}}
{{- $siteToSiteProvenance := default (dict) $observability.siteToSiteProvenance -}}
{{- $parameterContexts := default (dict) .Values.parameterContexts -}}
{{- $versionedFlowImports := default (dict) .Values.versionedFlowImports -}}
{{- if or (and $linkerd.enabled $istio.enabled) (and $linkerd.enabled $ambient.enabled) (and $istio.enabled $ambient.enabled) -}}
{{- fail "linkerd.enabled, istio.enabled, and ambient.enabled are mutually exclusive; choose one bounded service-mesh compatibility profile" -}}
{{- end -}}
{{- if $route.enabled -}}
{{- if not $route.host -}}
{{- fail "openshift.route.enabled=true requires openshift.route.host so the public Route hostname stays explicit and NiFi web.proxyHosts can be configured predictably" -}}
{{- end -}}
{{- $routeHostWithPort := printf "%s:443" $route.host -}}
{{- if and (not (has $route.host .Values.web.proxyHosts)) (not (has $routeHostWithPort .Values.web.proxyHosts)) -}}
{{- fail "openshift.route.enabled=true requires web.proxyHosts to include the public Route host or host:443" -}}
{{- end -}}
{{- end -}}
{{- if and (ne $mode "disabled") (ne $mode "nativeApi") (ne $mode "nativeApiLegacy") (ne $mode "exporter") (ne $mode "siteToSite") -}}
{{- fail "observability.metrics.mode must be one of: disabled, nativeApi, exporter, siteToSite" -}}
{{- end -}}
{{- if and $siteToSiteMetrics.enabled (ne $mode "siteToSite") -}}
{{- fail "observability.metrics.siteToSite.enabled=true requires observability.metrics.mode=siteToSite" -}}
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
{{- $destination := default (dict) $siteToSiteMetrics.destination -}}
{{- $destinationAuth := default (dict) $siteToSiteMetrics.auth -}}
{{- $destinationAuthSecretRef := default (dict) $destinationAuth.secretRef -}}
{{- $authorizedIdentity := trim (default "" $destinationAuth.authorizedIdentity) -}}
{{- $source := default (dict) $siteToSiteMetrics.source -}}
{{- $transport := default (dict) $siteToSiteMetrics.transport -}}
{{- $format := default (dict) $siteToSiteMetrics.format -}}
{{- if not $siteToSiteMetrics.enabled -}}
{{- fail "observability.metrics.siteToSite.enabled=true is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if ne (include "nifi.authMode" .) "singleUser" -}}
{{- fail "observability.metrics.mode=siteToSite currently requires auth.mode=singleUser because the typed bootstrap reconciles bounded NiFi runtime objects through the local NiFi API and does not introduce a generic management credential API" -}}
{{- end -}}
{{- if not $destination.url -}}
{{- fail "observability.metrics.siteToSite.destination.url is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if not (or (hasPrefix "https://" $destination.url) (hasPrefix "http://" $destination.url)) -}}
{{- fail "observability.metrics.siteToSite.destination.url must start with http:// or https://" -}}
{{- end -}}
{{- if not $destination.inputPortName -}}
{{- fail "observability.metrics.siteToSite.destination.inputPortName is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if eq $destinationAuth.type "" -}}
{{- fail "observability.metrics.siteToSite.auth.type is required when observability.metrics.mode=siteToSite" -}}
{{- end -}}
{{- if and (ne $destinationAuth.type "") (ne $destinationAuth.type "none") (ne $destinationAuth.type "workloadTLS") (ne $destinationAuth.type "secretRef") -}}
{{- fail "observability.metrics.siteToSite.auth.type must be one of: none, workloadTLS, secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $authorizedIdentity -}}
{{- fail "observability.metrics.siteToSite.auth.authorizedIdentity must be empty when auth.type=none" -}}
{{- end -}}
{{- if and (or (eq $destinationAuth.type "workloadTLS") (eq $destinationAuth.type "secretRef")) (not $authorizedIdentity) -}}
{{- fail "observability.metrics.siteToSite.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.name) -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.name is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystoreKey) -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.keystoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystorePasswordKey) -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.keystorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststoreKey) -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.truststoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststorePasswordKey) -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.truststorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "workloadTLS") $destinationAuthSecretRef.name -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.* cannot be set when auth.type=workloadTLS" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $destinationAuthSecretRef.name -}}
{{- fail "observability.metrics.siteToSite.auth.secretRef.* cannot be set when auth.type=none" -}}
{{- end -}}
{{- if and (hasPrefix "https://" $destination.url) (eq $destinationAuth.type "none") -}}
{{- fail "observability.metrics.siteToSite.auth.type=none cannot be used with an https:// destination.url; use workloadTLS or secretRef" -}}
{{- end -}}
{{- if and (hasPrefix "http://" $destination.url) (ne $destinationAuth.type "") (ne $destinationAuth.type "none") -}}
{{- fail "observability.metrics.siteToSite.auth.type must be none for an http:// destination.url" -}}
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
{{- fail "observability.metrics.siteToSite.format.type must be AmbariFormat for the typed site-to-site metrics feature; broader Record Writer ownership remains out of scope" -}}
{{- end -}}
{{- end -}}
{{- if $siteToSiteStatus.enabled -}}
{{- $destination := default (dict) $siteToSiteStatus.destination -}}
{{- $destinationAuth := default (dict) $siteToSiteStatus.auth -}}
{{- $destinationAuthSecretRef := default (dict) $destinationAuth.secretRef -}}
{{- $authorizedIdentity := trim (default "" $destinationAuth.authorizedIdentity) -}}
{{- $source := default (dict) $siteToSiteStatus.source -}}
{{- $transport := default (dict) $siteToSiteStatus.transport -}}
{{- if ne (include "nifi.authMode" .) "singleUser" -}}
{{- fail "observability.siteToSiteStatus.enabled=true currently requires auth.mode=singleUser because the typed bootstrap reconciles one fixed SiteToSiteStatusReportingTask through the local NiFi API and does not introduce a generic management credential API" -}}
{{- end -}}
{{- if not $destination.url -}}
{{- fail "observability.siteToSiteStatus.destination.url is required when observability.siteToSiteStatus.enabled=true" -}}
{{- end -}}
{{- if not (or (hasPrefix "https://" $destination.url) (hasPrefix "http://" $destination.url)) -}}
{{- fail "observability.siteToSiteStatus.destination.url must start with http:// or https://" -}}
{{- end -}}
{{- if not $destination.inputPortName -}}
{{- fail "observability.siteToSiteStatus.destination.inputPortName is required when observability.siteToSiteStatus.enabled=true" -}}
{{- end -}}
{{- if eq $destinationAuth.type "" -}}
{{- fail "observability.siteToSiteStatus.auth.type is required when observability.siteToSiteStatus.enabled=true" -}}
{{- end -}}
{{- if and (ne $destinationAuth.type "") (ne $destinationAuth.type "none") (ne $destinationAuth.type "workloadTLS") (ne $destinationAuth.type "secretRef") -}}
{{- fail "observability.siteToSiteStatus.auth.type must be one of: none, workloadTLS, secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $authorizedIdentity -}}
{{- fail "observability.siteToSiteStatus.auth.authorizedIdentity must be empty when auth.type=none" -}}
{{- end -}}
{{- if and (or (eq $destinationAuth.type "workloadTLS") (eq $destinationAuth.type "secretRef")) (not $authorizedIdentity) -}}
{{- fail "observability.siteToSiteStatus.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.name) -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.name is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystoreKey) -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.keystoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystorePasswordKey) -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.keystorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststoreKey) -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.truststoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststorePasswordKey) -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.truststorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "workloadTLS") $destinationAuthSecretRef.name -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.* cannot be set when auth.type=workloadTLS" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $destinationAuthSecretRef.name -}}
{{- fail "observability.siteToSiteStatus.auth.secretRef.* cannot be set when auth.type=none" -}}
{{- end -}}
{{- if and (hasPrefix "https://" $destination.url) (eq $destinationAuth.type "none") -}}
{{- fail "observability.siteToSiteStatus.auth.type=none cannot be used with an https:// destination.url; use workloadTLS or secretRef" -}}
{{- end -}}
{{- if and (hasPrefix "http://" $destination.url) (ne $destinationAuth.type "") (ne $destinationAuth.type "none") -}}
{{- fail "observability.siteToSiteStatus.auth.type must be none for an http:// destination.url" -}}
{{- end -}}
{{- if and (ne $transport.protocol "RAW") (ne $transport.protocol "HTTP") -}}
{{- fail "observability.siteToSiteStatus.transport.protocol must be one of: RAW, HTTP" -}}
{{- end -}}
{{- if not $transport.communicationsTimeout -}}
{{- fail "observability.siteToSiteStatus.transport.communicationsTimeout is required when observability.siteToSiteStatus.enabled=true" -}}
{{- end -}}
{{- if and $source.instanceUrl (not (or (hasPrefix "https://" $source.instanceUrl) (hasPrefix "http://" $source.instanceUrl))) -}}
{{- fail "observability.siteToSiteStatus.source.instanceUrl must start with http:// or https://" -}}
{{- end -}}
{{- end -}}
{{- if $siteToSiteProvenance.enabled -}}
{{- $destination := default (dict) $siteToSiteProvenance.destination -}}
{{- $destinationAuth := default (dict) $siteToSiteProvenance.auth -}}
{{- $destinationAuthSecretRef := default (dict) $destinationAuth.secretRef -}}
{{- $authorizedIdentity := trim (default "" $destinationAuth.authorizedIdentity) -}}
{{- $source := default (dict) $siteToSiteProvenance.source -}}
{{- $transport := default (dict) $siteToSiteProvenance.transport -}}
{{- $provenance := default (dict) $siteToSiteProvenance.provenance -}}
{{- if ne (include "nifi.authMode" .) "singleUser" -}}
{{- fail "observability.siteToSiteProvenance.enabled=true currently requires auth.mode=singleUser because the typed bootstrap reconciles one fixed SiteToSiteProvenanceReportingTask through the local NiFi API and does not introduce a generic management credential API" -}}
{{- end -}}
{{- if not $destination.url -}}
{{- fail "observability.siteToSiteProvenance.destination.url is required when observability.siteToSiteProvenance.enabled=true" -}}
{{- end -}}
{{- if not (or (hasPrefix "https://" $destination.url) (hasPrefix "http://" $destination.url)) -}}
{{- fail "observability.siteToSiteProvenance.destination.url must start with http:// or https://" -}}
{{- end -}}
{{- if not $destination.inputPortName -}}
{{- fail "observability.siteToSiteProvenance.destination.inputPortName is required when observability.siteToSiteProvenance.enabled=true" -}}
{{- end -}}
{{- if eq $destinationAuth.type "" -}}
{{- fail "observability.siteToSiteProvenance.auth.type is required when observability.siteToSiteProvenance.enabled=true" -}}
{{- end -}}
{{- if and (ne $destinationAuth.type "") (ne $destinationAuth.type "none") (ne $destinationAuth.type "workloadTLS") (ne $destinationAuth.type "secretRef") -}}
{{- fail "observability.siteToSiteProvenance.auth.type must be one of: none, workloadTLS, secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $authorizedIdentity -}}
{{- fail "observability.siteToSiteProvenance.auth.authorizedIdentity must be empty when auth.type=none" -}}
{{- end -}}
{{- if and (or (eq $destinationAuth.type "workloadTLS") (eq $destinationAuth.type "secretRef")) (not $authorizedIdentity) -}}
{{- fail "observability.siteToSiteProvenance.auth.authorizedIdentity is required for secure Site-to-Site receiver authorization" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.name) -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.name is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystoreKey) -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.keystoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.keystorePasswordKey) -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.keystorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststoreKey) -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.truststoreKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "secretRef") (not $destinationAuthSecretRef.truststorePasswordKey) -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.truststorePasswordKey is required when auth.type=secretRef" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "workloadTLS") $destinationAuthSecretRef.name -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.* cannot be set when auth.type=workloadTLS" -}}
{{- end -}}
{{- if and (eq $destinationAuth.type "none") $destinationAuthSecretRef.name -}}
{{- fail "observability.siteToSiteProvenance.auth.secretRef.* cannot be set when auth.type=none" -}}
{{- end -}}
{{- if and (hasPrefix "https://" $destination.url) (eq $destinationAuth.type "none") -}}
{{- fail "observability.siteToSiteProvenance.auth.type=none cannot be used with an https:// destination.url; use workloadTLS or secretRef" -}}
{{- end -}}
{{- if and (hasPrefix "http://" $destination.url) (ne $destinationAuth.type "") (ne $destinationAuth.type "none") -}}
{{- fail "observability.siteToSiteProvenance.auth.type must be none for an http:// destination.url" -}}
{{- end -}}
{{- if and (ne $transport.protocol "RAW") (ne $transport.protocol "HTTP") -}}
{{- fail "observability.siteToSiteProvenance.transport.protocol must be one of: RAW, HTTP" -}}
{{- end -}}
{{- if not $transport.communicationsTimeout -}}
{{- fail "observability.siteToSiteProvenance.transport.communicationsTimeout is required when observability.siteToSiteProvenance.enabled=true" -}}
{{- end -}}
{{- if and $source.instanceUrl (not (or (hasPrefix "https://" $source.instanceUrl) (hasPrefix "http://" $source.instanceUrl))) -}}
{{- fail "observability.siteToSiteProvenance.source.instanceUrl must start with http:// or https://" -}}
{{- end -}}
{{- if and (ne $provenance.startPosition "beginningOfStream") (ne $provenance.startPosition "endOfStream") -}}
{{- fail "observability.siteToSiteProvenance.provenance.startPosition must be one of: beginningOfStream, endOfStream" -}}
{{- end -}}
{{- end -}}
{{- if $parameterContexts.enabled -}}
{{- $authMode := include "nifi.authMode" . -}}
{{- $authz := default (dict) .Values.authz -}}
{{- $bootstrap := default (dict) $authz.bootstrap -}}
{{- if not $parameterContexts.mountPath -}}
{{- fail "parameterContexts.mountPath is required when parameterContexts.enabled=true" -}}
{{- end -}}
{{- if and (ne $authMode "singleUser") (ne $authMode "oidc") (ne $authMode "ldap") -}}
{{- fail "parameterContexts.enabled=true supports only auth.mode=singleUser, oidc, or ldap" -}}
{{- end -}}
{{- if and (or (eq $authMode "oidc") (eq $authMode "ldap")) (not $bootstrap.initialAdminIdentity) -}}
{{- fail "parameterContexts.enabled=true with auth.mode=oidc or auth.mode=ldap requires authz.bootstrap.initialAdminIdentity so the bounded trusted-proxy management identity is explicit" -}}
{{- end -}}
{{- if eq (len $parameterContexts.contexts) 0 -}}
{{- fail "parameterContexts.enabled=true requires parameterContexts.contexts to contain at least one context definition" -}}
{{- end -}}
{{- $contextNames := list -}}
{{- $attachmentTargets := list -}}
{{- range $contextIndex, $context := $parameterContexts.contexts -}}
{{- $parameters := default (list) $context.parameters -}}
{{- $providerRefs := default (list) $context.providerRefs -}}
{{- $attachments := default (list) $context.attachments -}}
{{- if not $context.name -}}
{{- fail (printf "parameterContexts.contexts[%d].name is required" $contextIndex) -}}
{{- end -}}
{{- if ne (trim $context.name) $context.name -}}
{{- fail (printf "parameterContexts.contexts[%d].name=%q must not have leading or trailing whitespace" $contextIndex $context.name) -}}
{{- end -}}
{{- if has $context.name $contextNames -}}
{{- fail (printf "parameterContexts.contexts[%d].name=%q is duplicated; context names must be unique" $contextIndex $context.name) -}}
{{- end -}}
{{- $contextNames = append $contextNames $context.name -}}
{{- if and (eq (len $parameters) 0) (eq (len $providerRefs) 0) -}}
{{- fail (printf "parameterContexts.contexts[%d] must define at least one parameter or providerRef" $contextIndex) -}}
{{- end -}}
{{- $parameterNames := list -}}
{{- range $parameterIndex, $parameter := $parameters -}}
{{- $secretRef := default (dict) $parameter.secretRef -}}
{{- $hasInlineValue := hasKey $parameter "value" -}}
{{- if not $parameter.name -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].name is required" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if ne (trim $parameter.name) $parameter.name -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].name=%q must not have leading or trailing whitespace" $contextIndex $parameterIndex $parameter.name) -}}
{{- end -}}
{{- if has $parameter.name $parameterNames -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].name=%q is duplicated within the same context" $contextIndex $parameterIndex $parameter.name) -}}
{{- end -}}
{{- $parameterNames = append $parameterNames $parameter.name -}}
{{- if and $hasInlineValue $secretRef.name -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d] supports either value or secretRef, not both" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if and (not $hasInlineValue) (not $secretRef.name) -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d] requires either value or secretRef" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if and (not $secretRef.name) $secretRef.key -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].secretRef.name is required when secretRef.key is set" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if and $secretRef.name (not $secretRef.key) -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].secretRef.key is required when secretRef.name is set" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if and $parameter.sensitive $hasInlineValue -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d] must use secretRef when sensitive=true" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- if and (not $parameter.sensitive) $secretRef.name -}}
{{- fail (printf "parameterContexts.contexts[%d].parameters[%d].secretRef is supported only when sensitive=true" $contextIndex $parameterIndex) -}}
{{- end -}}
{{- end -}}
{{- $providerNames := list -}}
{{- range $providerIndex, $providerRef := $providerRefs -}}
{{- if not $providerRef.name -}}
{{- fail (printf "parameterContexts.contexts[%d].providerRefs[%d].name is required" $contextIndex $providerIndex) -}}
{{- end -}}
{{- if ne (trim $providerRef.name) $providerRef.name -}}
{{- fail (printf "parameterContexts.contexts[%d].providerRefs[%d].name=%q must not have leading or trailing whitespace" $contextIndex $providerIndex $providerRef.name) -}}
{{- end -}}
{{- if has $providerRef.name $providerNames -}}
{{- fail (printf "parameterContexts.contexts[%d].providerRefs[%d].name=%q is duplicated within the same context" $contextIndex $providerIndex $providerRef.name) -}}
{{- end -}}
{{- $providerNames = append $providerNames $providerRef.name -}}
{{- end -}}
{{- $attachmentNames := list -}}
{{- range $attachmentIndex, $attachment := $attachments -}}
{{- if not $attachment.rootProcessGroupName -}}
{{- fail (printf "parameterContexts.contexts[%d].attachments[%d].rootProcessGroupName is required" $contextIndex $attachmentIndex) -}}
{{- end -}}
{{- if ne (trim $attachment.rootProcessGroupName) $attachment.rootProcessGroupName -}}
{{- fail (printf "parameterContexts.contexts[%d].attachments[%d].rootProcessGroupName=%q must not have leading or trailing whitespace" $contextIndex $attachmentIndex $attachment.rootProcessGroupName) -}}
{{- end -}}
{{- if has $attachment.rootProcessGroupName $attachmentNames -}}
{{- fail (printf "parameterContexts.contexts[%d].attachments[%d].rootProcessGroupName=%q is duplicated within the same context" $contextIndex $attachmentIndex $attachment.rootProcessGroupName) -}}
{{- end -}}
{{- if has $attachment.rootProcessGroupName $attachmentTargets -}}
{{- fail (printf "parameterContexts.contexts[%d].attachments[%d].rootProcessGroupName=%q is already declared by another context; each direct root-child process group target can be attached to at most one declared Parameter Context" $contextIndex $attachmentIndex $attachment.rootProcessGroupName) -}}
{{- end -}}
{{- $attachmentNames = append $attachmentNames $attachment.rootProcessGroupName -}}
{{- $attachmentTargets = append $attachmentTargets $attachment.rootProcessGroupName -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if $versionedFlowImports.enabled -}}
{{- $flowRegistryClients := default (dict) .Values.flowRegistryClients -}}
{{- $authz := default (dict) .Values.authz -}}
{{- $bootstrap := default (dict) $authz.bootstrap -}}
{{- $authzBundles := default (dict) $authz.bundles -}}
{{- $authzCapabilities := default (dict) $authz.capabilities -}}
{{- $mutableFlow := default (dict) $authzCapabilities.mutableFlow -}}
{{- $authMode := include "nifi.authMode" . -}}
{{- if not $flowRegistryClients.enabled -}}
{{- fail "versionedFlowImports.enabled=true currently requires flowRegistryClients.enabled=true so the selected prepared client definition can be resolved for bounded import reconciliation" -}}
{{- end -}}
{{- $knownPreparedClientNames := list -}}
{{- range $client := default (list) $flowRegistryClients.clients -}}
{{- $knownPreparedClientNames = append $knownPreparedClientNames $client.name -}}
{{- end -}}
{{- $knownParameterContextNames := list -}}
{{- range $context := default (list) $parameterContexts.contexts -}}
{{- $knownParameterContextNames = append $knownParameterContextNames $context.name -}}
{{- end -}}
{{- if not $versionedFlowImports.mountPath -}}
{{- fail "versionedFlowImports.mountPath is required when versionedFlowImports.enabled=true" -}}
{{- end -}}
{{- if and (or (eq $authMode "oidc") (eq $authMode "ldap")) (not $bootstrap.initialAdminIdentity) -}}
{{- fail "versionedFlowImports.enabled=true with auth.mode=oidc or auth.mode=ldap requires authz.bootstrap.initialAdminIdentity so the bounded trusted-proxy management identity is explicit" -}}
{{- end -}}
{{- if and (eq $authMode "singleUser") (not (or (and $mutableFlow.enabled $mutableFlow.includeInitialAdmin) $authzBundles.flowVersionManager.includeInitialAdmin)) -}}
{{- fail "versionedFlowImports.enabled=true with auth.mode=singleUser requires authz.capabilities.mutableFlow.enabled=true with includeInitialAdmin=true or authz.bundles.flowVersionManager.includeInitialAdmin=true" -}}
{{- end -}}
{{- if eq (len $versionedFlowImports.imports) 0 -}}
{{- fail "versionedFlowImports.enabled=true requires versionedFlowImports.imports to contain at least one import definition" -}}
{{- end -}}
{{- $importNames := list -}}
{{- $targetRootProcessGroupNames := list -}}
{{- range $importIndex, $import := $versionedFlowImports.imports -}}
{{- $parameterContextRefs := default (list) $import.parameterContextRefs -}}
{{- $target := default (dict) $import.target -}}
{{- if not $import.name -}}
{{- fail (printf "versionedFlowImports.imports[%d].name is required" $importIndex) -}}
{{- end -}}
{{- if ne (trim $import.name) $import.name -}}
{{- fail (printf "versionedFlowImports.imports[%d].name=%q must not have leading or trailing whitespace" $importIndex $import.name) -}}
{{- end -}}
{{- if has $import.name $importNames -}}
{{- fail (printf "versionedFlowImports.imports[%d].name=%q is duplicated; import names must be unique" $importIndex $import.name) -}}
{{- end -}}
{{- $importNames = append $importNames $import.name -}}
{{- if not $import.registryClientName -}}
{{- fail (printf "versionedFlowImports.imports[%d].registryClientName is required" $importIndex) -}}
{{- end -}}
{{- if ne (trim $import.registryClientName) $import.registryClientName -}}
{{- fail (printf "versionedFlowImports.imports[%d].registryClientName=%q must not have leading or trailing whitespace" $importIndex $import.registryClientName) -}}
{{- end -}}
{{- if not (has $import.registryClientName $knownPreparedClientNames) -}}
{{- fail (printf "versionedFlowImports.imports[%d].registryClientName=%q is not present in flowRegistryClients.clients[].name" $importIndex $import.registryClientName) -}}
{{- end -}}
{{- $preparedClient := dict -}}
{{- range $client := default (list) $flowRegistryClients.clients -}}
{{- if eq $client.name $import.registryClientName -}}
{{- $preparedClient = $client -}}
{{- end -}}
{{- end -}}
{{- $preparedClientGithub := default (dict) $preparedClient.github -}}
{{- $preparedClientGithubAuth := default (dict) $preparedClientGithub.auth -}}
{{- if and (ne $preparedClient.provider "github") (ne $preparedClient.provider "nifiRegistry") -}}
{{- fail (printf "versionedFlowImports.imports[%d].registryClientName=%q currently requires flowRegistryClients.clients[].provider=github or nifiRegistry for bounded runtime-managed import" $importIndex $import.registryClientName) -}}
{{- end -}}
{{- if and (eq $preparedClient.provider "github") $preparedClientGithubAuth.type (eq $preparedClientGithubAuth.type "appInstallation") -}}
{{- fail (printf "versionedFlowImports.imports[%d].registryClientName=%q currently supports github.auth.type none or personalAccessToken; appInstallation remains future work" $importIndex $import.registryClientName) -}}
{{- end -}}
{{- if not $import.bucket -}}
{{- fail (printf "versionedFlowImports.imports[%d].bucket is required" $importIndex) -}}
{{- end -}}
{{- if ne (trim $import.bucket) $import.bucket -}}
{{- fail (printf "versionedFlowImports.imports[%d].bucket=%q must not have leading or trailing whitespace" $importIndex $import.bucket) -}}
{{- end -}}
{{- if not $import.flowName -}}
{{- fail (printf "versionedFlowImports.imports[%d].flowName is required" $importIndex) -}}
{{- end -}}
{{- if ne (trim $import.flowName) $import.flowName -}}
{{- fail (printf "versionedFlowImports.imports[%d].flowName=%q must not have leading or trailing whitespace" $importIndex $import.flowName) -}}
{{- end -}}
{{- if not $import.version -}}
{{- fail (printf "versionedFlowImports.imports[%d].version is required" $importIndex) -}}
{{- end -}}
{{- if not (or (eq $import.version "latest") (regexMatch "^\\S+$" (printf "%v" $import.version))) -}}
{{- fail (printf "versionedFlowImports.imports[%d].version must be \"latest\" or a non-empty version identifier without whitespace" $importIndex) -}}
{{- end -}}
{{- if not $target.rootProcessGroupName -}}
{{- fail (printf "versionedFlowImports.imports[%d].target.rootProcessGroupName is required" $importIndex) -}}
{{- end -}}
{{- if ne (trim $target.rootProcessGroupName) $target.rootProcessGroupName -}}
{{- fail (printf "versionedFlowImports.imports[%d].target.rootProcessGroupName=%q must not have leading or trailing whitespace" $importIndex $target.rootProcessGroupName) -}}
{{- end -}}
{{- if eq $target.rootProcessGroupName "root" -}}
{{- fail (printf "versionedFlowImports.imports[%d].target.rootProcessGroupName=%q is reserved" $importIndex $target.rootProcessGroupName) -}}
{{- end -}}
{{- if has $target.rootProcessGroupName $targetRootProcessGroupNames -}}
{{- fail (printf "versionedFlowImports.imports[%d].target.rootProcessGroupName=%q is duplicated; root child target names must be unique" $importIndex $target.rootProcessGroupName) -}}
{{- end -}}
{{- $targetRootProcessGroupNames = append $targetRootProcessGroupNames $target.rootProcessGroupName -}}
{{- if gt (len $parameterContextRefs) 1 -}}
{{- fail (printf "versionedFlowImports.imports[%d] supports at most one direct parameterContextRef in this slice" $importIndex) -}}
{{- end -}}
{{- $parameterContextRefNames := list -}}
{{- range $refIndex, $ref := $parameterContextRefs -}}
{{- if not $ref.name -}}
{{- fail (printf "versionedFlowImports.imports[%d].parameterContextRefs[%d].name is required" $importIndex $refIndex) -}}
{{- end -}}
{{- if ne (trim $ref.name) $ref.name -}}
{{- fail (printf "versionedFlowImports.imports[%d].parameterContextRefs[%d].name=%q must not have leading or trailing whitespace" $importIndex $refIndex $ref.name) -}}
{{- end -}}
{{- if has $ref.name $parameterContextRefNames -}}
{{- fail (printf "versionedFlowImports.imports[%d].parameterContextRefs[%d].name=%q is duplicated within the same import" $importIndex $refIndex $ref.name) -}}
{{- end -}}
{{- $parameterContextRefNames = append $parameterContextRefNames $ref.name -}}
{{- if and $parameterContexts.enabled (not (has $ref.name $knownParameterContextNames)) -}}
{{- fail (printf "versionedFlowImports.imports[%d].parameterContextRefs[%d].name=%q is not present in parameterContexts.contexts[].name" $importIndex $refIndex $ref.name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
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
