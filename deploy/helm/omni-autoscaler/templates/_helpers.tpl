{{/*
Common helpers for the omni-autoscaler chart.

Kept deliberately tiny — the chart is a thin wrapper around a single
Deployment + ServiceAccount + Secret. Template complexity goes up
faster than operator readability, so we keep interpolation simple
and let Helm errors blame the right file when a required value is
missing.
*/}}

{{/*
Expand the name of the chart. Honors .Values.nameOverride when set so
operators can shorten "omni-autoscaler-omni-autoscaler-…" when
installing into a namespace where the chart name is obvious.
*/}}
{{- define "omni-autoscaler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Fully qualified app name — "<release-name>-<chart-name>" unless
.Values.fullnameOverride is set. Bounded to 63 chars so it can serve
as the Deployment name without tripping Kubernetes' DNS limit.
*/}}
{{- define "omni-autoscaler.fullname" -}}
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
Chart version label — "<chart-name>-<chart-version>". Applied to every
resource so `kubectl get -l app.kubernetes.io/chart=…` can filter on
the exact chart that produced a resource.
*/}}
{{- define "omni-autoscaler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Common labels applied to every resource. Matches the kubernetes.io
recommended label schema plus the experimental-feature callout so
operators grepping on `bearbinary.com/experimental=true` can find
every resource the chart creates.
*/}}
{{- define "omni-autoscaler.labels" -}}
helm.sh/chart: {{ include "omni-autoscaler.chart" . }}
{{ include "omni-autoscaler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
bearbinary.com/experimental: "true"
bearbinary.com/cluster: {{ .Values.cluster.name | quote }}
{{- end }}

{{/*
Selector labels — the subset of labels that land on both the Pod
template AND the Deployment selector. Must stay stable across
chart upgrades (changing a selector after the Deployment exists
forces a delete+recreate).
*/}}
{{- define "omni-autoscaler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "omni-autoscaler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name: honor .Values.serviceAccount.name when set,
otherwise mint one from the fullname. Matches the behavior of the
upstream bitnami/grafana/… charts so operators who have seen one
Helm chart know how to override this one.
*/}}
{{- define "omni-autoscaler.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "omni-autoscaler.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image reference: "<repo>:<tag>". Tag falls back to Chart.appVersion
when .Values.image.tag is empty so `helm upgrade --reuse-values`
doesn't silently pin a stale tag across chart upgrades.
*/}}
{{- define "omni-autoscaler.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end }}
