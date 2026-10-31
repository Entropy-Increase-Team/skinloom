{{/* Release-scoped name, e.g. "skinloom" or "<release>-skinloom". */}}
{{- define "skinloom.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "skinloom.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Selector of one component: server (also the all-in-one pod) or web. */}}
{{- define "skinloom.selector" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "skinloom.image" -}}
{{- printf "%s/%s:%s" .root.Values.image.registry .name (.root.Values.image.tag | default .root.Chart.AppVersion) -}}
{{- end -}}

{{/* The settings that make no sense together fail the install with a reason. */}}
{{- define "skinloom.validate" -}}
{{- if not (has .Values.mode (list "allInOne" "split")) -}}
{{- fail "mode must be allInOne or split" -}}
{{- end -}}
{{- if not (has .Values.database.driver (list "sqlite" "postgres" "mysql")) -}}
{{- fail "database.driver must be sqlite, postgres or mysql" -}}
{{- end -}}
{{- if and (ne .Values.database.driver "sqlite") (not .Values.database.existingSecret) -}}
{{- fail "database.existingSecret is required for postgres and mysql (a Secret with the DSN)" -}}
{{- end -}}
{{- $s3 := false -}}
{{- range .Values.server.extraEnv -}}
{{- if and (eq .name "SKINLOOM_STORAGE__DRIVER") (eq (toString .value) "s3") -}}
{{- $s3 = true -}}
{{- end -}}
{{- end -}}
{{- if not .Values.persistence.enabled -}}
{{- if eq .Values.database.driver "sqlite" -}}
{{- fail "persistence.enabled=false would keep the SQLite database on a temporary volume: every restart loses the site" -}}
{{- end -}}
{{- if not $s3 -}}
{{- fail "persistence.enabled=false needs textures in S3 (SKINLOOM_STORAGE__DRIVER=s3 in server.extraEnv): on a temporary volume every restart loses them" -}}
{{- end -}}
{{- end -}}
{{- if gt (int .Values.server.replicas) 1 -}}
{{- if eq .Values.database.driver "sqlite" -}}
{{- fail "server.replicas > 1 needs postgres or mysql: SQLite has one writer" -}}
{{- end -}}
{{- if and .Values.persistence.enabled (not .Values.persistence.existingClaim) (has "ReadWriteOnce" .Values.persistence.accessModes) -}}
{{- fail "server.replicas > 1 needs a ReadWriteMany volume or persistence.enabled=false (textures in S3)" -}}
{{- end -}}
{{- end -}}
{{- if and .Values.ingress.enabled (not .Values.ingress.hosts) -}}
{{- fail "ingress.enabled needs at least one host in ingress.hosts" -}}
{{- end -}}
{{- if and (eq .Values.mode "allInOne") (gt (int .Values.server.replicas) 1) -}}
{{- fail "allInOne runs one pod; use mode=split to scale" -}}
{{- end -}}
{{- end -}}

{{- define "skinloom.podSecurity" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end -}}
