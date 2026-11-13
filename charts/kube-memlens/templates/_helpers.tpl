{{- define "kube-memlens.image" -}}
{{- if .Values.image.digest -}}
{{- if not (regexMatch "^sha256:[a-f0-9]{64}$" .Values.image.digest) -}}
{{- fail "image.digest must be an exact sha256 digest" -}}
{{- end -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{/*
The workloads go into namespace.name, which need not be the release namespace.
Helm creates only the release namespace, so a missing target would otherwise
surface as a bare "namespace not found" halfway through the install.
lookup returns nothing without a cluster, so the check runs only when
kube-system is visible; helm template and client dry runs are unaffected.
*/}}
{{- define "kube-memlens.requireNamespace" -}}
{{- $target := .Values.namespace.name -}}
{{- if and (not .Values.namespace.create) (ne $target .Release.Namespace) -}}
{{- if and (lookup "v1" "Namespace" "" "kube-system") (not (lookup "v1" "Namespace" "" $target)) -}}
{{- fail (printf "namespace.name is %q, but that namespace does not exist and the release namespace is %q. Install with --namespace %s --create-namespace, or set namespace.name=%s, or set namespace.create=true." $target .Release.Namespace $target .Release.Namespace) -}}
{{- end -}}
{{- end -}}
{{- end -}}
