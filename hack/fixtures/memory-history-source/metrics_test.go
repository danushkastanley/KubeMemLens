package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scrapeFixture(t *testing.T, config configuration, method, token string) *httptest.ResponseRecorder {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	f := fixture{path: path, token: "fixture-credential"}
	req := httptest.NewRequest(method, "/metrics", nil)
	req.Header.Set("Authorization", token)
	response := httptest.NewRecorder()
	f.metrics(response, req)
	return response
}

func scrapeConfig() configuration {
	return configuration{Mode: "normal", Series: []series{
		{Labels: map[string]string{"cluster": "fixture", "node": "node", "node_uid": "node-uid", "id": "/"}, WorkingSet: 0, RSS: 512},
		{Labels: map[string]string{"cluster": "fixture", "node": "node", "node_uid": "node-uid", "namespace": "team", "pod": "pod", "pod_uid": "pod-uid", "container": "app", "container_id": "containerd://current"}, WorkingSet: 1048576, RSS: 524288},
	}}
}

func TestScrapeGaugesKeepZeroAndUseScrapeTime(t *testing.T) {
	response := scrapeFixture(t, scrapeConfig(), http.MethodGet, "Bearer fixture-credential")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain;") {
		t.Fatal("scrape failed", response.Code)
	}
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	if len(lines) != 6 || !strings.HasSuffix(lines[2], " 0") || !strings.HasSuffix(lines[5], " 524288") {
		t.Fatal("gauge values or cardinality changed")
	}
	for _, line := range lines[2:] {
		if len(strings.Fields(line)) != 2 {
			t.Fatal("fixture must not supply timestamps")
		}
	}
}

func TestScrapeRejectsUnauthorisedOrNonGET(t *testing.T) {
	for _, input := range [][2]string{{http.MethodGet, ""}, {http.MethodGet, "Bearer wrong"}, {http.MethodPost, "Bearer fixture-credential"}} {
		if r := scrapeFixture(t, scrapeConfig(), input[0], input[1]); r.Code != http.StatusForbidden || strings.Contains(r.Body.String(), "container_memory") {
			t.Fatal("unauthorised scrape disclosed evidence")
		}
	}
}

func TestScrapeRejectsInvalidEvidenceBeforeWritingGauges(t *testing.T) {
	for name, mutate := range map[string]func(*configuration){
		"missing-identity": func(c *configuration) { delete(c.Series[1].Labels, "pod_uid") },
		"root-with-pod":    func(c *configuration) { c.Series[0].Labels["pod"] = "pod" },
		"unknown-label":    func(c *configuration) { c.Series[1].Labels["untrusted"] = "value" },
		"control-label":    func(c *configuration) { c.Series[1].Labels["pod"] = "pod\nother" },
		"oversized-label":  func(c *configuration) { c.Series[1].Labels["pod"] = strings.Repeat("x", 1025) },
		"duplicate":        func(c *configuration) { c.Series = append(c.Series, c.Series[0]) },
		"too-many":         func(c *configuration) { c.Series = make([]series, 33) },
		"failure-mode":     func(c *configuration) { c.Mode = "unavailable" },
		"empty":            func(c *configuration) { c.Series = nil },
	} {
		t.Run(name, func(t *testing.T) {
			config := scrapeConfig()
			mutate(&config)
			r := scrapeFixture(t, config, http.MethodGet, "Bearer fixture-credential")
			if r.Code != http.StatusServiceUnavailable || strings.Contains(r.Body.String(), "container_memory") {
				t.Fatal("invalid scrape emitted partial evidence")
			}
		})
	}
}
