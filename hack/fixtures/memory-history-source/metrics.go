package main

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// metrics exposes controlled gauges to a real evaluator. Prometheus assigns
// scrape timestamps; the fixture never manufactures historical samples here.
func (f *fixture) metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+f.token)) != 1 {
		http.Error(w, "denied", http.StatusForbidden)
		return
	}
	config, err := f.configuration()
	if err != nil || config.Mode != "normal" || len(config.Series) == 0 {
		http.Error(w, "scrape fixture unavailable", http.StatusServiceUnavailable)
		return
	}
	var body strings.Builder
	body.WriteString("# TYPE container_memory_working_set_bytes gauge\n# TYPE container_memory_rss gauge\n")
	seen := make(map[string]bool, len(config.Series))
	for _, s := range config.Series {
		labels, err := metricLabels(s.Labels)
		if err != nil || seen[labels] {
			http.Error(w, "invalid scrape fixture", http.StatusServiceUnavailable)
			return
		}
		seen[labels] = true
		fmt.Fprintf(&body, "container_memory_working_set_bytes{%s} %d\ncontainer_memory_rss{%s} %d\n", labels, s.WorkingSet, labels, s.RSS)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprint(w, body.String())
}

func metricLabels(labels map[string]string) (string, error) {
	invalid := fmt.Errorf("invalid immutable fixture identity")
	if len(labels) > 9 || labels["cluster"] == "" || labels["node"] == "" || labels["node_uid"] == "" {
		return "", invalid
	}
	keys := make([]string, 0, len(labels))
	for key, value := range labels {
		switch key {
		case "cluster", "node", "node_uid", "namespace", "pod", "pod_uid", "container", "container_id", "id":
		default:
			return "", invalid
		}
		if len(value) > 1024 || strings.ContainsFunc(value, unicode.IsControl) {
			return "", invalid
		}
		keys = append(keys, key)
	}
	if labels["id"] == "/" {
		for _, key := range []string{"namespace", "pod", "pod_uid", "container", "container_id"} {
			if labels[key] != "" {
				return "", invalid
			}
		}
	} else {
		if labels["id"] != "" {
			return "", invalid
		}
		for _, key := range []string{"namespace", "pod", "pod_uid", "container", "container_id"} {
			if labels[key] == "" {
				return "", invalid
			}
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+strconv.Quote(labels[key]))
	}
	return strings.Join(parts, ","), nil
}
