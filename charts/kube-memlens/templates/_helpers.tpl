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

{{/*
Who creates the serving CA, certificate and key: the chart while it renders, or
the certificate bootstrap Job in the cluster. Values stored by an older release
have no source and keep the chart.
*/}}
{{- define "kube-memlens.tlsSource" -}}
{{- .Values.extensionTLS.source | default "chart" -}}
{{- end -}}

{{/*
The bootstrap keeps its Secret under its own name. Helm deletes the
chart-rendered Secret when a release switches source, and the collector must
already have the other one to mount.
*/}}
{{- define "kube-memlens.tlsSecretName" -}}
{{- if eq (include "kube-memlens.tlsSource" .) "bootstrap" -}}
{{- printf "%s-bootstrap" .Values.extensionTLS.secretName -}}
{{- else -}}
{{- .Values.extensionTLS.secretName -}}
{{- end -}}
{{- end -}}
