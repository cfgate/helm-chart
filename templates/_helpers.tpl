{{/*
Expand the name of the chart.
*/}}
{{- define "cfgate.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Pin the default operator to the chart-owned digest, not a value retained by
--reuse-values. Explicit repository/tag overrides preserve existing behavior.
*/}}
{{- define "cfgate.image" -}}
{{- $digest := .Values.image.digest | default "" -}}
{{- if and (not $digest) (eq .Values.image.repository "ghcr.io/cfgate/cfgate") (not .Values.image.tag) -}}
{{- $digest = required "chart operator image digest is required" (index .Chart.Annotations "cfgate.io/operator-image-digest") -}}
{{- end -}}
{{- if $digest -}}
{{- if not (regexMatch "^sha256:[a-f0-9]{64}$" $digest) -}}
{{- fail "operator image digest must be sha256 followed by 64 lowercase hex characters" -}}
{{- end -}}
{{- printf "%s@%s" .Values.image.repository $digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "cfgate.fullname" -}}
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
{{- define "cfgate.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Define namespace (supports override for multi-namespace deployments).
*/}}
{{- define "cfgate.namespace" -}}
{{- if .Values.namespaceOverride }}
{{- .Values.namespaceOverride }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/* Keep claim permissions in the namespace selected by the manager. */}}
{{- define "cfgate.installationNamespace" -}}
{{- $controller := .Values.controller | default dict -}}
{{- default (include "cfgate.namespace" .) $controller.installationNamespace -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "cfgate.labels" -}}
helm.sh/chart: {{ include "cfgate.chart" . }}
{{ include "cfgate.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: controller
{{- end }}

{{/*
Selector labels
*/}}
{{- define "cfgate.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cfgate.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "cfgate.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cfgate.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
