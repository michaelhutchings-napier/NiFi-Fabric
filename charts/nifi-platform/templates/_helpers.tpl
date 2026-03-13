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
{{- end -}}
{{- end -}}
