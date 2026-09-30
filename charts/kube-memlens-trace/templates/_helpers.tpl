{{- define "trace.name" -}}
{{- $identity := printf "%s/%s" .Release.Namespace .Release.Name -}}
{{- printf "%s-%s-trace" (.Release.Name | trunc 20 | trimSuffix "-") ($identity | sha256sum | trunc 8) -}}
{{- end -}}

{{- define "trace.image" -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- end -}}

{{- define "trace.validate" -}}
{{- if .Values.enabled -}}
{{- if or (ne .Values.profile "development-linux-containerd") (not .Values.acknowledgeUnqualifiedDevelopment) -}}
{{- fail "only explicitly acknowledged, unqualified development tracing is available" -}}
{{- end -}}
{{- if has .Release.Namespace (list "default" "kube-system" "kube-public" "kube-node-lease" "kube-memlens") -}}
{{- fail "trace installation requires a separate administrator-owned namespace" -}}
{{- end -}}
{{- $names := dict -}}
{{- $uids := dict -}}
{{- $ids := dict -}}
{{- range .Values.nodes -}}
{{- range splitList "." .name -}}
{{- if or (gt (len .) 63) (not (regexMatch "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" .)) -}}
{{- fail "trace node name has an invalid DNS label" -}}
{{- end -}}
{{- end -}}
{{- range splitList "/" .kubeletCgroupRoot -}}
{{- if gt (len .) 63 -}}{{ fail "kubelet cgroup root component exceeds 63 bytes" }}{{- end -}}
{{- end -}}
{{- if or (hasKey $names .name) (hasKey $uids .uid) (hasKey $ids .id) -}}
{{- fail "trace nodes require unique ids, names and UIDs" -}}
{{- end -}}
{{- $_ := set $names .name true -}}
{{- $_ := set $uids .uid true -}}
{{- $_ := set $ids .id true -}}
{{- end -}}
{{- end -}}
{{- end -}}
