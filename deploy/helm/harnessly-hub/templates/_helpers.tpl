{{/* Chart name. */}}
{{- define "hub.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified app name, truncated to the 63 characters DNS names allow. */}}
{{- define "hub.fullname" -}}
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

{{- define "hub.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "hub.imageTag" -}}
{{- .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{- define "hub.labels" -}}
helm.sh/chart: {{ include "hub.chart" . }}
{{ include "hub.selectorLabels" . }}
app.kubernetes.io/version: {{ include "hub.imageTag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "hub.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hub.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "hub.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "hub.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* The Secret the pod reads: the user's, or the one this chart creates. */}}
{{- define "hub.secretName" -}}
{{- default (include "hub.fullname" .) .Values.existingSecret }}
{{- end }}

{{/* HUB_PUBLIC_URL, which the chart cannot guess. */}}
{{- define "hub.publicUrl" -}}
{{- required "publicUrl is required, e.g. --set publicUrl=https://hub.example.com" .Values.publicUrl | trimSuffix "/" }}
{{- end }}

{{/* The host of the public URL: the ingress host. */}}
{{- define "hub.host" -}}
{{- (urlParse (include "hub.publicUrl" .)).hostname }}
{{- end }}

{{/* Names of the bundled Postgres. */}}
{{- define "hub.postgresName" -}}
{{- printf "%s-postgres" (include "hub.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* The Secret that holds the bundled Postgres password under `password`. */}}
{{- define "hub.postgresSecretName" -}}
{{- default (include "hub.postgresName" .) .Values.database.postgres.existingSecret }}
{{- end }}

{{/* Refuses the settings that would put an open hub on the internet. Included at the top of the Deployment. */}}
{{- define "hub.validate" -}}
{{- if and .Values.ingress.enabled (not .Values.adminEmails) }}
{{- fail "adminEmails is empty while the ingress is on: the first account to sign in would become the administrator, and with the ingress that can be anyone. List the administrators' addresses, e.g. --set 'adminEmails[0]=you@example.com'" }}
{{- end }}
{{- if and (eq .Values.edition "cloud") (not .Values.runtimeDomain) }}
{{- fail "edition: cloud needs runtimeDomain: machines must run on an origin of their own, not on the hub's (e.g. --set runtimeDomain=run.example.net, a domain that is not a sibling of the hub's)" }}
{{- end }}
{{- if and .Values.docs.enabled (not .Values.docs.artifacts.bundled) (not .Values.docs.artifacts.url) }}
{{- fail "docs.enabled needs an artifacts service: set docs.artifacts.bundled=true, or docs.artifacts.url to one you run (and docs.existingSecret with its token)" }}
{{- end }}
{{- if and .Values.docs.enabled .Values.docs.boards.bundled (not .Values.docs.boards.image.repository) }}
{{- fail "docs.boards.bundled needs docs.boards.image.repository: no boards image is published, build it from the boards repository" }}
{{- end }}
{{- if and .Values.docs.enabled (or (and (not .Values.docs.artifacts.bundled) .Values.docs.artifacts.url) (and (not .Values.docs.boards.bundled) .Values.docs.boards.url)) (not .Values.docs.existingSecret) }}
{{- fail "docs.existingSecret is required with docs.artifacts.url or docs.boards.url: the chart cannot know the token of a service it does not run" }}
{{- end }}
{{- end }}

{{- define "hub.docsName" -}}
{{- printf "%s-docs" (include "hub.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- define "hub.docsSecretName" -}}
{{- default (include "hub.docsName" .) .Values.docs.existingSecret }}
{{- end }}
{{- define "hub.artifactsURL" -}}
{{- if .Values.docs.artifacts.bundled }}{{ printf "http://%s-artifacts:8080" (include "hub.fullname" .) }}{{ else }}{{ .Values.docs.artifacts.url }}{{ end }}
{{- end }}
{{- define "hub.boardsURL" -}}
{{- if .Values.docs.boards.bundled }}{{ printf "http://%s-boards:8080" (include "hub.fullname" .) }}{{ else }}{{ .Values.docs.boards.url }}{{ end }}
{{- end }}

{{/* Several replicas run only on a database that is shared: the in-cluster Postgres or one named by database.external. */}}
{{- define "hub.multi" -}}
{{- if and (gt (int .Values.replicaCount) 1) (or .Values.database.postgres.enabled .Values.database.external) -}}true{{- end -}}
{{- end }}

{{- define "hub.replicaSecretName" -}}
{{- printf "%s-replicas" (include "hub.fullname" .) -}}
{{- end }}
