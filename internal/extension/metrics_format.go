package extension

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

// metricsFormat is the representation chosen for the metrics/current resource.
type metricsFormat int

const (
	// metricsJSON is the Kubernetes-style envelope used by kubectl and the CLI.
	metricsJSON metricsFormat = iota
	// metricsOpenMetrics is the exposition text for scrapers that ask for it.
	metricsOpenMetrics
	// metricsPrometheusText is the same text for scrapers that only accept the
	// classic Prometheus text format.
	metricsPrometheusText
)

const prometheusTextContentType = "text/plain; version=0.0.4; charset=utf-8"

// negotiateMetricsFormat picks the metrics representation from an Accept
// header. Scrapers only receive exposition text when they list a supported
// text media type with a quality at least as high as JSON. A missing header,
// */* and anything unrecognised keep the JSON envelope, so existing readers
// are unaffected.
func negotiateMetricsFormat(accept string) metricsFormat {
	if strings.TrimSpace(accept) == "" {
		return metricsJSON
	}
	// Ties prefer explicit JSON, then OpenMetrics, then Prometheus text, then a
	// wildcard, which also means JSON.
	best, bestQuality, bestRank := metricsJSON, 0.0, 0
	for _, part := range strings.Split(accept, ",") {
		format, rank, quality, ok := parseMetricsMediaRange(part)
		if !ok || quality <= 0 {
			continue
		}
		if quality > bestQuality || quality == bestQuality && rank > bestRank {
			best, bestQuality, bestRank = format, quality, rank
		}
	}
	return best
}

func parseMetricsMediaRange(part string) (metricsFormat, int, float64, bool) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(part))
	if err != nil {
		return metricsJSON, 0, 0, false
	}
	quality := 1.0
	if raw, found := params["q"]; found {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return metricsJSON, 0, 0, false
		}
		quality = parsed
	}
	version := params["version"]
	switch mediaType {
	case "application/json":
		return metricsJSON, 4, quality, true
	case "application/openmetrics-text":
		// The renderer writes OpenMetrics 1.0.0; 0.0.1 parses the same way.
		return metricsOpenMetrics, 3, quality, version == "" || version == "1.0.0" || version == "0.0.1"
	case "text/plain":
		return metricsPrometheusText, 2, quality, version == "" || version == "0.0.4"
	case "*/*", "application/*":
		return metricsJSON, 1, quality, true
	default:
		return metricsJSON, 0, 0, false
	}
}

// writeMetricsText writes exposition text that the exporter has already
// bounded to the nested read budget.
func writeMetricsText(w http.ResponseWriter, format metricsFormat, content string) {
	contentType := metrics.ContentType
	if format == metricsPrometheusText {
		contentType = prometheusTextContentType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}
