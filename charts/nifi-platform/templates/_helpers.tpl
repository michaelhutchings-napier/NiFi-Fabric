{{- define "nifi-platform.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "nifi-platform.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "nifi-platform.labels" -}}
app.kubernetes.io/name: {{ include "nifi-platform.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "nifi-platform.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nifi-platform.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "nifi-platform.mode" -}}
{{- $mode := default "standalone" .Values.mode -}}
{{- if not (has $mode (list "standalone" "managed" "managed-cert-manager")) -}}
{{- fail "mode must be one of: standalone, managed, managed-cert-manager" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{- define "nifi-platform.managedMode" -}}
{{- $mode := include "nifi-platform.mode" . -}}
{{- if or (eq $mode "managed") (eq $mode "managed-cert-manager") -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "nifi-platform.nifiName" -}}
{{- default "nifi" .Values.nifi.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.nifiFullname" -}}
{{- if .Values.nifi.fullnameOverride -}}
{{- .Values.nifi.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "nifi-platform.nifiName" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "nifi-platform.controllerNamespace" -}}
{{- default "nifi-system" .Values.controller.namespace.name -}}
{{- end -}}

{{- define "nifi-platform.controllerName" -}}
{{- printf "%s-controller-manager" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.controllerServiceAccountName" -}}
{{- default (include "nifi-platform.controllerName" .) .Values.controller.serviceAccount.name -}}
{{- end -}}

{{- define "nifi-platform.clusterName" -}}
{{- default (include "nifi-platform.nifiFullname" .) .Values.cluster.name -}}
{{- end -}}

{{- define "nifi-platform.targetRefName" -}}
{{- default (include "nifi-platform.nifiFullname" .) .Values.cluster.targetRef.name -}}
{{- end -}}

{{- define "nifi-platform.tlsSecretName" -}}
{{- $mode := dig "tls" "mode" "externalSecret" .Values.nifi -}}
{{- if eq $mode "certManager" -}}
{{- default "nifi-tls" (dig "tls" "certManager" "secretName" "" .Values.nifi) -}}
{{- else -}}
{{- default "nifi-tls" (dig "tls" "existingSecret" "" .Values.nifi) -}}
{{- end -}}
{{- end -}}

{{- define "nifi-platform.trustManagerBundleName" -}}
{{- printf "%s-trust-bundle" (include "nifi-platform.nifiFullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.trustManagerTargetType" -}}
{{- default "configMap" .Values.trustManager.target.type -}}
{{- end -}}

{{- define "nifi-platform.trustManagerMirrorTrustNamespace" -}}
{{- default "cert-manager" .Values.trustManager.mirrorTLSSecret.trustNamespace -}}
{{- end -}}

{{- define "nifi-platform.trustManagerMirrorSourceSecretName" -}}
{{- default (include "nifi-platform.tlsSecretName" .) .Values.trustManager.mirrorTLSSecret.sourceSecretName -}}
{{- end -}}

{{- define "nifi-platform.trustManagerMirrorTargetSecretName" -}}
{{- default (printf "%s-tls-ca-source" (include "nifi-platform.nifiFullname" .)) .Values.trustManager.mirrorTLSSecret.targetSecretName -}}
{{- end -}}

{{- define "nifi-platform.trustManagerMirrorSecretKey" -}}
{{- default "ca.crt" .Values.trustManager.mirrorTLSSecret.targetKey -}}
{{- end -}}

{{- define "nifi-platform.trustManagerMirrorName" -}}
{{- printf "%s-trust-source-mirror" (include "nifi-platform.nifiFullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.quickstartEnabled" -}}
{{- if .Values.quickstart.enabled -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{- define "nifi-platform.quickstartKubectlImage" -}}
{{- $image := default (dict) .Values.quickstart.tls.kubectlImage -}}
{{- if and $image.digest $image.repository -}}
{{- printf "%s@%s" $image.repository $image.digest -}}
{{- else -}}
{{- printf "%s:%s" $image.repository (default "latest" $image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "nifi-platform.quickstartTLSBootstrapName" -}}
{{- printf "%s-quickstart-tls-bootstrap" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "nifi-platform.validate" -}}
{{- $mode := include "nifi-platform.mode" . -}}
{{- $managed := eq (include "nifi-platform.managedMode" .) "true" -}}
{{- $controllerManaged := dig "controllerManaged" "enabled" false .Values.nifi -}}
{{- if and $managed (not $controllerManaged) -}}
{{- fail "managed modes require nifi.controllerManaged.enabled=true so the subchart renders the OnDelete-managed StatefulSet" -}}
{{- end -}}
{{- if and (not $managed) $controllerManaged -}}
{{- fail "standalone mode requires nifi.controllerManaged.enabled=false" -}}
{{- end -}}
{{- if and (eq $mode "managed-cert-manager") (ne (dig "tls" "mode" "externalSecret" .Values.nifi) "certManager") -}}
{{- fail "mode=managed-cert-manager requires nifi.tls.mode=certManager" -}}
{{- end -}}
{{- if and (eq $mode "managed-cert-manager") (not (dig "tls" "certManager" "enabled" false .Values.nifi)) -}}
{{- fail "mode=managed-cert-manager requires nifi.tls.certManager.enabled=true" -}}
{{- end -}}
{{- if .Values.quickstart.enabled -}}
{{- if not $managed -}}
{{- fail "quickstart.enabled=true requires a managed platform mode" -}}
{{- end -}}
{{- if ne (dig "auth" "mode" "singleUser" .Values.nifi) "singleUser" -}}
{{- fail "quickstart.enabled=true requires nifi.auth.mode=singleUser; OIDC and LDAP stay on the explicit operator-provided Secret path" -}}
{{- end -}}
{{- if not (dig "auth" "singleUser" "existingSecret" "" .Values.nifi) -}}
{{- fail "quickstart.enabled=true requires nifi.auth.singleUser.existingSecret so the generated quickstart auth Secret has a stable name" -}}
{{- end -}}
{{- if lt (int (default 24 .Values.quickstart.singleUser.passwordLength)) 12 -}}
{{- fail "quickstart.singleUser.passwordLength must be at least 12" -}}
{{- end -}}
{{- $tlsMode := dig "tls" "mode" "externalSecret" .Values.nifi -}}
{{- if and (eq $tlsMode "externalSecret") (not (dig "tls" "existingSecret" "" .Values.nifi)) -}}
{{- fail "quickstart.enabled=true with nifi.tls.mode=externalSecret requires nifi.tls.existingSecret so the generated quickstart TLS Secret has a stable name" -}}
{{- end -}}
{{- if and (eq $tlsMode "externalSecret") (dig "tls" "sensitiveProps" "secretRef" "name" "" .Values.nifi) -}}
{{- fail "quickstart.enabled=true with nifi.tls.mode=externalSecret requires nifi.tls.sensitiveProps.secretRef.name to stay empty so the generated quickstart TLS Secret can carry the sensitive props key" -}}
{{- end -}}
{{- if and (eq $tlsMode "certManager") (not (dig "tls" "certManager" "enabled" false .Values.nifi)) -}}
{{- fail "quickstart.enabled=true with nifi.tls.mode=certManager requires nifi.tls.certManager.enabled=true" -}}
{{- end -}}
{{- if and (eq $tlsMode "certManager") (not (or (dig "tls" "certManager" "pkcs12" "password" "" .Values.nifi) (dig "tls" "certManager" "pkcs12" "passwordSecretRef" "name" "" .Values.nifi))) -}}
{{- fail "quickstart.enabled=true with nifi.tls.mode=certManager requires either an inline PKCS12 password or nifi.tls.certManager.pkcs12.passwordSecretRef.name" -}}
{{- end -}}
{{- if and (eq $tlsMode "certManager") (not (or (dig "tls" "sensitiveProps" "value" "" .Values.nifi) (dig "tls" "sensitiveProps" "secretRef" "name" "" .Values.nifi))) -}}
{{- fail "quickstart.enabled=true with nifi.tls.mode=certManager requires either nifi.tls.sensitiveProps.value or nifi.tls.sensitiveProps.secretRef.name" -}}
{{- end -}}
{{- if lt (int (default 24 .Values.quickstart.tls.passwordLength)) 12 -}}
{{- fail "quickstart.tls.passwordLength must be at least 12" -}}
{{- end -}}
{{- if lt (int (default 32 .Values.quickstart.tls.sensitivePropsKeyLength)) 16 -}}
{{- fail "quickstart.tls.sensitivePropsKeyLength must be at least 16" -}}
{{- end -}}
{{- if lt (int (default 365 .Values.quickstart.tls.validityDays)) 1 -}}
{{- fail "quickstart.tls.validityDays must be greater than 0" -}}
{{- end -}}
{{- $kubectlImage := default (dict) .Values.quickstart.tls.kubectlImage -}}
{{- if not $kubectlImage.repository -}}
{{- fail "quickstart.enabled=true requires quickstart.tls.kubectlImage.repository" -}}
{{- end -}}
{{- if and (not $kubectlImage.tag) (not $kubectlImage.digest) -}}
{{- fail "quickstart.enabled=true requires quickstart.tls.kubectlImage.tag or quickstart.tls.kubectlImage.digest" -}}
{{- end -}}
{{- end -}}
{{- if .Values.trustManager.enabled -}}
{{- if not $managed -}}
{{- fail "trustManager.enabled=true requires a managed platform mode" -}}
{{- end -}}
{{- $sources := default (dict) .Values.trustManager.sources -}}
{{- $sourceConfigMaps := default (list) $sources.configMaps -}}
{{- $sourceSecrets := default (list) $sources.secrets -}}
{{- $sourceInline := default (list) $sources.inline -}}
{{- $mirror := default (dict) .Values.trustManager.mirrorTLSSecret -}}
{{- $mirrorEnabled := default false $mirror.enabled -}}
{{- $target := default (dict) .Values.trustManager.target -}}
{{- $targetType := default "configMap" $target.type -}}
{{- if and (ne $targetType "configMap") (ne $targetType "secret") -}}
{{- fail "trustManager.target.type must be one of: configMap, secret" -}}
{{- end -}}
{{- if and (not $sources.useDefaultCAs) (eq (len $sourceConfigMaps) 0) (eq (len $sourceSecrets) 0) (eq (len $sourceInline) 0) (not $mirrorEnabled) -}}
{{- fail "trustManager.enabled=true requires at least one source: useDefaultCAs, sources.configMaps, sources.secrets, sources.inline, or mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- range $index, $source := $sourceConfigMaps -}}
{{- if not $source.name -}}
{{- fail (printf "trustManager.sources.configMaps[%d].name is required" $index) -}}
{{- end -}}
{{- if not $source.key -}}
{{- fail (printf "trustManager.sources.configMaps[%d].key is required" $index) -}}
{{- end -}}
{{- end -}}
{{- range $index, $source := $sourceSecrets -}}
{{- if not $source.name -}}
{{- fail (printf "trustManager.sources.secrets[%d].name is required" $index) -}}
{{- end -}}
{{- if not $source.key -}}
{{- fail (printf "trustManager.sources.secrets[%d].key is required" $index) -}}
{{- end -}}
{{- end -}}
{{- range $index, $source := $sourceInline -}}
{{- if not $source.pem -}}
{{- fail (printf "trustManager.sources.inline[%d].pem is required" $index) -}}
{{- end -}}
{{- end -}}
{{- if not .Values.trustManager.target.key -}}
{{- fail "trustManager.target.key is required when trustManager.enabled=true" -}}
{{- end -}}
{{- $pkcs12 := default (dict) $target.additionalFormats.pkcs12 -}}
{{- if $pkcs12.enabled -}}
{{- if not $pkcs12.key -}}
{{- fail "trustManager.target.additionalFormats.pkcs12.key is required when PKCS12 output is enabled" -}}
{{- end -}}
{{- if and $pkcs12.profile (not (has $pkcs12.profile (list "LegacyRC2" "LegacyDES" "Modern2023"))) -}}
{{- fail "trustManager.target.additionalFormats.pkcs12.profile must be one of: LegacyRC2, LegacyDES, Modern2023" -}}
{{- end -}}
{{- end -}}
{{- $jks := default (dict) $target.additionalFormats.jks -}}
{{- if $jks.enabled -}}
{{- if not $jks.key -}}
{{- fail "trustManager.target.additionalFormats.jks.key is required when JKS output is enabled" -}}
{{- end -}}
{{- if not $jks.password -}}
{{- fail "trustManager.target.additionalFormats.jks.password is required when JKS output is enabled" -}}
{{- end -}}
{{- end -}}
{{- if $mirrorEnabled -}}
{{- $mirrorImage := default (dict) .Values.trustManager.mirrorTLSSecret.image -}}
{{- if not $mirrorImage.repository -}}
{{- fail "trustManager.mirrorTLSSecret.image.repository is required when mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- if and (not $mirrorImage.tag) (not $mirrorImage.digest) -}}
{{- fail "trustManager.mirrorTLSSecret.image.tag or trustManager.mirrorTLSSecret.image.digest is required when mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- if not $mirror.targetKey -}}
{{- fail "trustManager.mirrorTLSSecret.targetKey is required when mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- if not $mirror.sourceKey -}}
{{- fail "trustManager.mirrorTLSSecret.sourceKey is required when mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- if not $mirror.schedule -}}
{{- fail "trustManager.mirrorTLSSecret.schedule is required when mirrorTLSSecret.enabled=true" -}}
{{- end -}}
{{- if and (eq (include "nifi-platform.trustManagerMirrorTrustNamespace" .) .Release.Namespace) (eq (include "nifi-platform.trustManagerMirrorSourceSecretName" .) (include "nifi-platform.trustManagerMirrorTargetSecretName" .)) -}}
{{- fail "trustManager.mirrorTLSSecret cannot target the same Secret name in the release namespace as its source" -}}
{{- end -}}
{{- end -}}
{{- $nifiTrustManagerRef := default (dict) .Values.nifi.trustManagerBundleRef -}}
{{- $nifiTrustManagerRefType := default "configMap" $nifiTrustManagerRef.type -}}
{{- $nifiTrustManagerRefKey := default "ca.crt" $nifiTrustManagerRef.key -}}
{{- $nifiUsesTrustManagerBundle := or (dig "tls" "additionalTrustBundle" "useTrustManagerBundle" false .Values.nifi) (dig "observability" "metrics" "nativeApi" "tlsConfig" "ca" "useTrustManagerBundle" false .Values.nifi) (dig "observability" "metrics" "exporter" "source" "tlsConfig" "ca" "useTrustManagerBundle" false .Values.nifi) -}}
{{- if $nifiUsesTrustManagerBundle -}}
{{- if ne $nifiTrustManagerRefType $targetType -}}
{{- fail "nifi.trustManagerBundleRef.type must match trustManager.target.type when the app chart consumes the platform trust-manager bundle" -}}
{{- end -}}
{{- if ne $nifiTrustManagerRefKey $target.key -}}
{{- fail "nifi.trustManagerBundleRef.key must match trustManager.target.key when the app chart consumes the platform trust-manager bundle" -}}
{{- end -}}
{{- if and $nifiTrustManagerRef.name (ne $nifiTrustManagerRef.name (include "nifi-platform.trustManagerBundleName" .)) -}}
{{- fail "nifi.trustManagerBundleRef.name must match the platform-generated trust bundle name when the app chart consumes the platform trust-manager bundle" -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if .Values.keda.enabled -}}
{{- if not $managed -}}
{{- fail "keda.enabled=true requires a managed platform mode because KEDA targets NiFiCluster, not the standalone StatefulSet" -}}
{{- end -}}
{{- if not .Values.cluster.create -}}
{{- fail "keda.enabled=true requires cluster.create=true so the chart renders the NiFiCluster target" -}}
{{- end -}}
{{- if ne .Values.cluster.autoscaling.mode "Enforced" -}}
{{- fail "keda.enabled=true currently requires cluster.autoscaling.mode=Enforced" -}}
{{- end -}}
{{- if not .Values.cluster.autoscaling.external.enabled -}}
{{- fail "keda.enabled=true requires cluster.autoscaling.external.enabled=true" -}}
{{- end -}}
{{- if ne .Values.cluster.autoscaling.external.source "KEDA" -}}
{{- fail "keda.enabled=true requires cluster.autoscaling.external.source=KEDA" -}}
{{- end -}}
{{- if and .Values.cluster.autoscaling.external.scaleDownEnabled (not .Values.cluster.autoscaling.scaleDown.enabled) -}}
{{- fail "cluster.autoscaling.external.scaleDownEnabled=true requires cluster.autoscaling.scaleDown.enabled=true so the controller can reuse the safe scale-down pipeline" -}}
{{- end -}}
{{- if and (not .Values.cluster.autoscaling.scaleUp.enabled) (not (and .Values.cluster.autoscaling.external.scaleDownEnabled .Values.cluster.autoscaling.scaleDown.enabled)) -}}
{{- fail "keda.enabled=true requires at least one supported controller-mediated direction: cluster.autoscaling.scaleUp.enabled=true or cluster.autoscaling.external.scaleDownEnabled=true with cluster.autoscaling.scaleDown.enabled=true" -}}
{{- end -}}
{{- if not .Values.keda.triggers -}}
{{- fail "keda.enabled=true requires at least one trigger in keda.triggers" -}}
{{- end -}}
{{- if lt (len .Values.keda.triggers) 1 -}}
{{- fail "keda.enabled=true requires at least one trigger in keda.triggers" -}}
{{- end -}}
{{- if lt (int .Values.keda.maxReplicaCount) (int .Values.keda.minReplicaCount) -}}
{{- fail "keda.maxReplicaCount must be greater than or equal to keda.minReplicaCount" -}}
{{- end -}}
{{- if ne (int .Values.cluster.autoscaling.external.requestedReplicas) 0 -}}
{{- fail "keda.enabled=true requires cluster.autoscaling.external.requestedReplicas=0 because the NiFiCluster /scale field is runtime-managed by KEDA and the controller" -}}
{{- end -}}
{{- if lt (int .Values.keda.minReplicaCount) (int .Values.cluster.autoscaling.minReplicas) -}}
{{- fail "keda.minReplicaCount must be greater than or equal to cluster.autoscaling.minReplicas so KEDA intent stays within the controller-owned autoscaling floor" -}}
{{- end -}}
{{- if and .Values.cluster.autoscaling.external.scaleDownEnabled (ne (int .Values.keda.minReplicaCount) (int .Values.cluster.autoscaling.minReplicas)) -}}
{{- fail "keda.minReplicaCount must equal cluster.autoscaling.minReplicas when cluster.autoscaling.external.scaleDownEnabled=true so KEDA's inactive floor matches the controller-owned safe downscale floor" -}}
{{- end -}}
{{- if gt (int .Values.keda.maxReplicaCount) (int .Values.cluster.autoscaling.maxReplicas) -}}
{{- fail "keda.maxReplicaCount must be less than or equal to cluster.autoscaling.maxReplicas so KEDA intent stays within the controller-owned autoscaling ceiling" -}}
{{- end -}}
{{- end -}}
{{- end -}}
