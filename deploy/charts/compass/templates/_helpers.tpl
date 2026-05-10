{{/*
Expand the name of the chart.
*/}}
{{- define "compass.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "compass.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "compass.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "compass.labels" -}}
helm.sh/chart: {{ include "compass.chart" . }}
{{ include "compass.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "compass.selectorLabels" -}}
app.kubernetes.io/name: {{ include "compass.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "compass.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "compass.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the object that holds compass.yaml. When the user supplies an
existing ConfigMap or Secret we mount that one verbatim; otherwise the
chart generates a ConfigMap named {fullname}-config.
*/}}
{{- define "compass.configObjectName" -}}
{{- if and .Values.existingConfigMap.name .Values.existingSecret.name }}
{{- fail "existingConfigMap.name and existingSecret.name are mutually exclusive" }}
{{- else if .Values.existingSecret.name }}
{{- .Values.existingSecret.name }}
{{- else if .Values.existingConfigMap.name }}
{{- .Values.existingConfigMap.name }}
{{- else }}
{{- printf "%s-config" (include "compass.fullname" .) }}
{{- end }}
{{- end }}

{{- define "compass.configMapName" -}}
{{- include "compass.configObjectName" . }}
{{- end }}

{{- define "compass.configVolumeKind" -}}
{{- if .Values.existingSecret.name -}}secret{{- else -}}configMap{{- end -}}
{{- end }}

{{/*
Resolved name of the ConfigMap that backs `pages.dir`. Returns the
external name when set, otherwise the chart-generated `{fullname}-pages`.
*/}}
{{- define "compass.pagesConfigMapName" -}}
{{- if .Values.pages.existingConfigMap.name }}
{{- .Values.pages.existingConfigMap.name }}
{{- else }}
{{- printf "%s-pages" (include "compass.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Resolved name of the ConfigMap that backs `catalog.path`.
*/}}
{{- define "compass.catalogConfigMapName" -}}
{{- if .Values.catalog.existingConfigMap.name }}
{{- .Values.catalog.existingConfigMap.name }}
{{- else }}
{{- printf "%s-catalog" (include "compass.fullname" .) }}
{{- end }}
{{- end }}

{{/*
True when the chart should mount a pages volume — either pages.enabled
is true (with inline files or an external ConfigMap) or the user pointed
at an existingConfigMap explicitly.
*/}}
{{- define "compass.pagesMountActive" -}}
{{- if or .Values.pages.enabled .Values.pages.existingConfigMap.name -}}true{{- end -}}
{{- end }}

{{- define "compass.catalogMountActive" -}}
{{- if or .Values.catalog.enabled .Values.catalog.existingConfigMap.name -}}true{{- end -}}
{{- end }}
