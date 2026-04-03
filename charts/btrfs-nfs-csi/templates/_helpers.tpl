{{/*
Expand the name of the chart.
*/}}
{{- define "btrfs-nfs-csi.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "btrfs-nfs-csi.fullname" -}}
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
{{- define "btrfs-nfs-csi.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "btrfs-nfs-csi.labels" -}}
helm.sh/chart: {{ include "btrfs-nfs-csi.chart" . }}
{{ include "btrfs-nfs-csi.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "btrfs-nfs-csi.selectorLabels" -}}
app.kubernetes.io/name: {{ include "btrfs-nfs-csi.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Controller name
*/}}
{{- define "btrfs-nfs-csi.controllerName" -}}
{{- printf "%s-controller" (include "btrfs-nfs-csi.fullname" .) }}
{{- end }}

{{/*
Driver name
*/}}
{{- define "btrfs-nfs-csi.driverName" -}}
{{- printf "%s-driver" (include "btrfs-nfs-csi.fullname" .) }}
{{- end }}

{{/*
CSI driver image
*/}}
{{- define "btrfs-nfs-csi.driverImage" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/*
Controller service account name
*/}}
{{- define "btrfs-nfs-csi.controllerServiceAccountName" -}}
{{- if .Values.serviceAccount.controller.create }}
{{- default (include "btrfs-nfs-csi.controllerName" .) .Values.serviceAccount.controller.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.controller.name }}
{{- end }}
{{- end }}

{{/*
Driver service account name
*/}}
{{- define "btrfs-nfs-csi.driverServiceAccountName" -}}
{{- if .Values.serviceAccount.driver.create }}
{{- default (include "btrfs-nfs-csi.driverName" .) .Values.serviceAccount.driver.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.driver.name }}
{{- end }}
{{- end }}

{{/*
Health check enabled (returns "1" if interval is set and not "0")
*/}}
{{- define "btrfs-nfs-csi.healthCheckEnabled" -}}
{{- if and .Values.driver.healthCheckInterval (ne .Values.driver.healthCheckInterval "0") }}1{{- else }}0{{- end }}
{{- end }}
