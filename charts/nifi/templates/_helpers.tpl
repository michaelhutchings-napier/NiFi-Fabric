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
