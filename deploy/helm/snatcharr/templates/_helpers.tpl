{{/*
SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
SPDX-License-Identifier: EUPL-1.2
*/}}

{{- define "snatcharr.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "snatcharr.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "snatcharr.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "snatcharr.labels" -}}
helm.sh/chart: {{ include "snatcharr.chart" . }}
app.kubernetes.io/name: {{ include "snatcharr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: snatcharr
{{- end -}}

{{/* Selector labels for one component: pass (dict "root" . "component" "api") */}}
{{- define "snatcharr.selectorLabels" -}}
app.kubernetes.io/name: {{ include "snatcharr.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "snatcharr.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "snatcharr.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "snatcharr.secretName" -}}
{{- default (printf "%s-secrets" (include "snatcharr.fullname" .)) .Values.secrets.existingSecret -}}
{{- end -}}

{{- define "snatcharr.dbClusterName" -}}
{{- printf "%s-db" (include "snatcharr.fullname" .) -}}
{{- end -}}

{{/* Secret + key that hold APP_DB_DSN. */}}
{{- define "snatcharr.dbSecretName" -}}
{{- if .Values.postgres.cnpg.enabled -}}
{{- printf "%s-app" (include "snatcharr.dbClusterName" .) -}}
{{- else -}}
{{- required "postgres.external.dsnSecretRef.name is required when postgres.cnpg.enabled=false" .Values.postgres.external.dsnSecretRef.name -}}
{{- end -}}
{{- end -}}

{{- define "snatcharr.dbSecretKey" -}}
{{- if .Values.postgres.cnpg.enabled -}}uri{{- else -}}{{ .Values.postgres.external.dsnSecretRef.key }}{{- end -}}
{{- end -}}

{{- define "snatcharr.apiImage" -}}
{{- printf "%s:%s" .Values.image.api.repository (default .Chart.AppVersion .Values.image.api.tag) -}}
{{- end -}}

{{- define "snatcharr.workerImage" -}}
{{- printf "%s:%s" .Values.image.worker.repository (default .Chart.AppVersion .Values.image.worker.tag) -}}
{{- end -}}

{{- define "snatcharr.apiServiceName" -}}
{{- printf "%s-api" (include "snatcharr.fullname" .) -}}
{{- end -}}

{{/*
snatcharr.arrEgressPeers renders the ipBlock peers for networkPolicy.arrCidrs. An IPv4 CIDR
that contains the cloud metadata address 169.254.169.254 excludes it. Kubernetes accepts an
`except` only as a strict subset of its `cidr`, so a CIDR that does not contain the address
(10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) gets no `except`, and a /32 on the address
itself is refused. An IPv6 CIDR cannot contain it and gets none either.
*/}}
{{- define "snatcharr.arrEgressPeers" -}}
{{- $meta := 0 -}}
{{- range list 169 254 169 254 -}}{{- $meta = add (mul $meta 256) . -}}{{- end -}}
{{- $peers := list -}}
{{- range .Values.networkPolicy.arrCidrs -}}
{{- $peer := dict "cidr" . -}}
{{- if not (contains ":" .) -}}
{{- $parts := splitList "/" . -}}
{{- $bits := 32 -}}
{{- if eq (len $parts) 2 -}}{{- $bits = atoi (index $parts 1) -}}{{- end -}}
{{- $net := 0 -}}
{{- range splitList "." (index $parts 0) -}}{{- $net = add (mul $net 256) (atoi .) -}}{{- end -}}
{{- $scale := 1 -}}
{{- range until (int (sub 32 $bits)) -}}{{- $scale = mul $scale 2 -}}{{- end -}}
{{- if eq (div $net $scale) (div $meta $scale) -}}
{{- if eq $bits 32 -}}
{{- fail (printf "networkPolicy.arrCidrs: %s is the cloud metadata address, which egress never allows" .) -}}
{{- end -}}
{{- $_ := set $peer "except" (list "169.254.169.254/32") -}}
{{- end -}}
{{- end -}}
{{- $peers = append $peers (dict "ipBlock" $peer) -}}
{{- end -}}
{{- toYaml $peers -}}
{{- end -}}
