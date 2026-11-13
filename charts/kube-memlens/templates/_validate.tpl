{{/*
Converts a schema duration (one whole number and one of ms, s, m or h) to
milliseconds, so two durations in different units can be compared.
*/}}
{{- define "kube-memlens.durationMillis" -}}
{{- $unit := regexFind "[a-z]+$" . -}}
{{- $count := int64 (trimSuffix $unit .) -}}
{{- mul $count (get (dict "ms" 1 "s" 1000 "m" 60000 "h" 3600000) $unit) -}}
{{- end -}}

{{/*
Refuses value combinations that each pass the schema on their own but break
once installed. Rendered from validate.yaml so every render runs them.
*/}}
{{- define "kube-memlens.validateValues" -}}
{{- if .Values.collector.enabled -}}
{{- $validity := mul (int64 .Values.extensionTLS.validityDays) 86400000 -}}
{{- if ge (int64 (include "kube-memlens.durationMillis" .Values.extensionTLS.rotateBefore)) $validity -}}
{{- fail (printf "extensionTLS.rotateBefore (%s) must be shorter than extensionTLS.validityDays (%d days), or the certificate would always be due for rotation" .Values.extensionTLS.rotateBefore (int .Values.extensionTLS.validityDays)) -}}
{{- end -}}
{{- if eq (int .Values.collector.service.port) (int .Values.collector.ingestion.extensionPort) -}}
{{- fail (printf "collector.service.port and collector.ingestion.extensionPort are both %d; the collector's health and extension listeners need different ports" (int .Values.collector.service.port)) -}}
{{- end -}}
{{- if .Values.agent.enabled -}}
{{- if lt (int64 (include "kube-memlens.durationMillis" .Values.collector.snapshotTTL)) (int64 (include "kube-memlens.durationMillis" .Values.agent.interval)) -}}
{{- fail (printf "collector.snapshotTTL (%s) must not be shorter than agent.interval (%s), or every Node would always be reported as stale" .Values.collector.snapshotTTL .Values.agent.interval) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- if and .Values.metrics.prometheusRule.enabled (not .Values.metrics.enabled) -}}
{{- fail "metrics.prometheusRule.enabled requires metrics.enabled; without metrics the collector-unavailable alert would fire permanently" -}}
{{- end -}}
{{- end -}}
