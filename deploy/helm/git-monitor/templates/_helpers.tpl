{{- define "git-monitor.name" -}}
{{- .Chart.Name -}}
{{- end -}}

{{- define "git-monitor.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "git-monitor.labels" -}}
app.kubernetes.io/name: {{ include "git-monitor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "git-monitor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "git-monitor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "git-monitor.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "git-monitor.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "git-monitor.webhookSecretName" -}}
{{- if .Values.argocd.webhookPasswordSecretName -}}
{{- .Values.argocd.webhookPasswordSecretName -}}
{{- else -}}
{{- printf "%s-webhook" (include "git-monitor.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "git-monitor.webhookSecretKey" -}}
{{- if .Values.argocd.webhookPasswordSecretName -}}
{{- default "password" .Values.argocd.webhookPasswordSecretKey -}}
{{- else -}}
password
{{- end -}}
{{- end -}}
