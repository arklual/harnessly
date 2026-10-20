{{/* Chart name. */}}
{{- define "harnessly.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified app name, truncated to the 63 characters DNS names allow. */}}
{{- define "harnessly.fullname" -}}
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

{{- define "harnessly.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "harnessly.imageTag" -}}
{{- .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "harnessly.labels" -}}
helm.sh/chart: {{ include "harnessly.chart" . }}
{{ include "harnessly.selectorLabels" . }}
app.kubernetes.io/version: {{ include "harnessly.imageTag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "harnessly.selectorLabels" -}}
app.kubernetes.io/name: {{ include "harnessly.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "harnessly.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "harnessly.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* The Secret the pod reads: the user's, or the one this chart creates. */}}
{{- define "harnessly.secretName" -}}
{{- default (include "harnessly.fullname" .) .Values.existingSecret }}
{{- end }}

{{/* The base path as the runtime takes it: "/" or "/path" without a trailing slash. */}}
{{- define "harnessly.basePath" -}}
{{- $p := trimSuffix "/" (.Values.basePath | default "/") }}
{{- if eq $p "" }}/{{ else if hasPrefix "/" $p }}{{ $p }}{{ else }}/{{ $p }}{{ end }}
{{- end }}

{{/* The health route under the base path. */}}
{{- define "harnessly.healthPath" -}}
{{- $p := include "harnessly.basePath" . }}
{{- if eq $p "/" }}/api/health{{ else }}{{ $p }}/api/health{{ end }}
{{- end }}
