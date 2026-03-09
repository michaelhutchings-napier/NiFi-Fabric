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
{{- join "," $hosts -}}
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
