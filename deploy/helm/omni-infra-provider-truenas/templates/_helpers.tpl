{{/*
Chart name, truncated to 63 chars.
*/}}
{{- define "omni-infra-provider-truenas.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name, truncated to 63 chars.
*/}}
{{- define "omni-infra-provider-truenas.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "omni-infra-provider-truenas.labels" -}}
app.kubernetes.io/name: {{ include "omni-infra-provider-truenas.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: omni
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "omni-infra-provider-truenas.selectorLabels" -}}
app.kubernetes.io/name: {{ include "omni-infra-provider-truenas.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Secret name — use existingSecret if set, otherwise the generated fullname.
*/}}
{{- define "omni-infra-provider-truenas.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- include "omni-infra-provider-truenas.fullname" . }}
{{- end }}
{{- end }}

{{/*
Image tag — defaults to v<Chart.AppVersion>.
*/}}
{{- define "omni-infra-provider-truenas.imageTag" -}}
{{- .Values.image.tag | default (printf "v%s" .Chart.AppVersion) }}
{{- end }}

{{/*
Health port — parses the numeric port from healthListenAddr (e.g. ":8081" -> 8081).
*/}}
{{- define "omni-infra-provider-truenas.healthPort" -}}
{{- .Values.healthListenAddr | trimPrefix ":" | int }}
{{- end }}
