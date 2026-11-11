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
{{- if or (hasKey .Values.docs "artifacts") (kindIs "map" .Values.docs.boards) }}
{{- fail "docs.artifacts.* and docs.boards.* moved: documents are docs.* (enabled, url, bundled, image, storage, auth, existingSecret) and boards are boards.* with the same keys; see docs/apps/documents.md and docs/apps/boards.md" }}
{{- end }}
{{- range $key := list "docs" "boards" }}
{{- $v := index $.Values $key }}
{{- if $v.enabled }}
{{- if not (regexMatch "^https?://[^/]+" ($v.url | default "")) }}
{{- fail (printf "%s.enabled needs %s.url: the address machines and browsers open the service at, e.g. --set %s.url=https://%s.example.com" $key $key $key $key) }}
{{- end }}
{{- if not (has $v.auth (list "token" "harnessly")) }}
{{- fail (printf "%s.auth is %q: token or harnessly" $key $v.auth) }}
{{- end }}
{{- if $v.bundled }}
{{- if not (has $v.storage.type (list "fs" "s3")) }}
{{- fail (printf "%s.bundled runs the service in this release and needs %s.storage.type: fs (a volume of its own, one replica) or s3 (with %s.storage.s3.existingSecret)" $key $key $key) }}
{{- end }}
{{- if and (eq $v.storage.type "s3") (not $v.storage.s3.existingSecret) }}
{{- fail (printf "%s.storage.type=s3 needs %s.storage.s3.existingSecret: a Secret with the service's S3 settings" $key $key) }}
{{- end }}
{{- if not (and $v.image.repository $v.image.tag) }}
{{- fail (printf "%s.bundled needs %s.image.repository and %s.image.tag" $key $key $key) }}
{{- end }}
{{- else if not $v.existingSecret }}
{{- fail (printf "%s.existingSecret is required with %s.bundled=false: the chart cannot know the token of a service it does not run" $key $key) }}
{{- end }}
{{- end }}
{{- end }}
{{- if not (has .Values.strategy.type (list "" "Recreate" "RollingUpdate")) }}
{{- fail (printf "strategy.type is %q: leave it empty (RollingUpdate with several replicas on a shared Postgres, Recreate otherwise), or set Recreate or RollingUpdate" .Values.strategy.type) }}
{{- end }}
{{- if and (eq .Values.strategy.type "RollingUpdate") (not (include "hub.multi" .)) }}
{{- fail "strategy.type: RollingUpdate needs several replicas on a shared database: set replicaCount to 2 or more and database.postgres.enabled=true (or database.external=true with HUB_DATABASE_URL in the Secret). On the SQLite file the old and the new pod would open one file together and corrupt it, and with one replica the machines have nowhere to go while it is replaced" }}
{{- end }}
{{- if include "hub.multi" . }}
{{- $extra := list }}
{{- range .Values.extraEnv }}{{- if and (eq .name "HUB_REPLICAS") (has (lower (toString .value)) (list "off" "false" "0" "no")) }}{{- $extra = append $extra .name }}{{- end }}{{- end }}
{{- if $extra }}
{{- fail "extraEnv turns HUB_REPLICAS off while replicaCount is above 1 on a shared database: the replicas would not find each other's machines. Set replicaCount=1, or drop HUB_REPLICAS from extraEnv" }}
{{- end }}
{{- if le (int (include "hub.shutdownGrace" .)) (add (int .Values.drain.settleSeconds) (int .Values.drain.linksSeconds)) }}
{{- fail (printf "terminationGracePeriodSeconds (%d) is too short for the drain: it must be more than drain.settleSeconds + drain.linksSeconds + 5 (%d), so the requests under way have time left" (int .Values.terminationGracePeriodSeconds) (add (int .Values.drain.settleSeconds) (int .Values.drain.linksSeconds) 5)) }}
{{- end }}
{{- end }}
{{- end }}

{{/* The Secret of docs.* or boards.* (dict "root" . "key" "docs"): existingSecret, or the one the chart makes. */}}
{{- define "hub.appSecretName" -}}
{{- $v := index .root.Values .key }}
{{- default (printf "%s-%s" (include "hub.fullname" .root) .key | trunc 63 | trimSuffix "-") $v.existingSecret }}
{{- end }}

{{/* Several replicas run only on a database that is shared: the in-cluster Postgres or one named by database.external. */}}
{{- define "hub.multi" -}}
{{- if and (gt (int .Values.replicaCount) 1) (or .Values.database.postgres.enabled .Values.database.external) -}}true{{- end -}}
{{- end }}

{{- define "hub.replicaSecretName" -}}
{{- printf "%s-replicas" (include "hub.fullname" .) -}}
{{- end }}

{{/* The strategy of the Deployment: the one asked for, else rolling for several replicas and replacing for one. */}}
{{- define "hub.strategy" -}}
{{- if .Values.strategy.type }}{{ .Values.strategy.type }}{{ else if include "hub.multi" . }}RollingUpdate{{ else }}Recreate{{ end }}
{{- end }}

{{/* Seconds the hub gives its own stop: the pod's grace less a margin for the kubelet. */}}
{{- define "hub.shutdownGrace" -}}
{{- sub (int .Values.terminationGracePeriodSeconds) 5 }}
{{- end }}

{{/*
The pod's affinity: the one given, else rules that keep the hub's pods apart from each other (several replicas) and
from the pod of the bundled Postgres, so one node lost does not take the hub and its database, or every replica.
*/}}
{{- define "hub.affinity" -}}
{{- if .Values.affinity }}
{{- toYaml .Values.affinity }}
{{- else if and .Values.antiAffinity.enabled (or (include "hub.multi" .) .Values.database.postgres.enabled) }}
{{- $terms := list }}
{{- if include "hub.multi" . }}
{{- $terms = append $terms (dict "topologyKey" .Values.antiAffinity.topologyKey "labelSelector" (dict "matchLabels" (dict "app.kubernetes.io/name" (include "hub.name" .) "app.kubernetes.io/instance" .Release.Name) "matchExpressions" (list (dict "key" "app.kubernetes.io/component" "operator" "DoesNotExist")))) }}
{{- end }}
{{- if .Values.database.postgres.enabled }}
{{- $terms = append $terms (dict "topologyKey" .Values.antiAffinity.topologyKey "labelSelector" (dict "matchLabels" (dict "app.kubernetes.io/name" (include "hub.name" .) "app.kubernetes.io/instance" .Release.Name "app.kubernetes.io/component" "postgres"))) }}
{{- end }}
podAntiAffinity:
  {{- if .Values.antiAffinity.required }}
  requiredDuringSchedulingIgnoredDuringExecution:
    {{- toYaml $terms | nindent 4 }}
  {{- else }}
  preferredDuringSchedulingIgnoredDuringExecution:
    {{- range $terms }}
    - weight: 100
      podAffinityTerm:
        {{- toYaml . | nindent 8 }}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}
