package resourcemetrics

import (
	"net/http"
	"strings"
	"testing"
)

func metricNode(name string, usage map[string]string) map[string]any {
	return map[string]any{"metadata": map[string]string{"name": name, "uid": "uid-" + name}, "timestamp": sampleTime, "window": "15s", "usage": usage}
}

func nodeAPIHandler(t *testing.T, versions []string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer caller-token" {
			t.Error("caller identity or read-only method changed")
		}
		if r.URL.Path == "/apis/metrics.k8s.io" {
			jsonResponse(t, w, groupBody(versions...))
			return
		}
		for _, version := range versions {
			if r.URL.Path == "/apis/metrics.k8s.io/"+version {
				jsonResponse(t, w, map[string]any{"groupVersion": group + "/" + version, "resources": []any{map[string]any{"name": "nodes", "namespaced": false, "verbs": []string{"get", "list"}}}})
				return
			}
		}
		handler(w, r)
	}
}

func TestNamedNodeMetricsUseAdvertisedVersionAndCallerIdentity(t *testing.T) {
	for _, versions := range [][]string{{"v1beta1", "v1"}, {"v1beta1"}} {
		version := versions[len(versions)-1]
		var names []string
		source := newTestSource(t, nodeAPIHandler(t, versions, func(w http.ResponseWriter, r *http.Request) {
			prefix := "/apis/metrics.k8s.io/" + version + "/nodes/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				t.Errorf("unexpected node path %s", r.URL)
				w.WriteHeader(500)
				return
			}
			name := strings.TrimPrefix(r.URL.Path, prefix)
			names = append(names, name)
			value := metricNode(name, map[string]string{"memory": "32Mi", "cpu": "0"})
			value["apiVersion"], value["kind"] = group+"/"+version, "NodeMetrics"
			jsonResponse(t, w, value)
		}), Options{})
		report, err := source.ReadNodes(t.Context(), []string{"node-b", "node-a", "node-a"})
		if err != nil || report.Availability != Available || len(report.Observations) != 2 || strings.Join(names, ",") != "node-a,node-b" {
			t.Fatalf("%+v %v names=%v", report, err, names)
		}
		value := report.Observations[0]
		if value.APIVersion != group+"/"+version || value.MemoryWorkingSetBytes != 32<<20 || !value.CPUUsageKnown || value.Timestamp != sampleTime || value.NodeUID != "uid-node-a" {
			t.Fatalf("%+v", value)
		}
	}
}

func TestNodeMetricsMissingAndDeniedEvidence(t *testing.T) {
	for _, status := range []int{404, 403} {
		source := newTestSource(t, nodeAPIHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "node-b") {
				w.WriteHeader(status)
				return
			}
			value := metricNode("node-a", map[string]string{"memory": "1Mi"})
			value["apiVersion"], value["kind"] = group+"/v1", "NodeMetrics"
			jsonResponse(t, w, value)
		}), Options{})
		report, err := source.ReadNodes(t.Context(), []string{"node-a", "node-b"})
		if err != nil {
			t.Fatal(err)
		}
		if status == 404 && (report.Availability != Partial || report.OmittedNodes != 1 || len(report.Observations) != 1 || report.Observations[0].CPUUsageKnown) {
			t.Fatalf("%+v", report)
		}
		if status == 403 && (report.Availability != Forbidden || len(report.Observations) != 0) {
			t.Fatal("node denial retained earlier results")
		}
	}
}

func TestNodeMetricsListsAreExplicitPagedAndBounded(t *testing.T) {
	for _, maximum := range []int{1, 5} {
		reads := 0
		source := newClusterTestSource(t, nodeAPIHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/apis/metrics.k8s.io/v1/nodes" {
				t.Errorf("unexpected list %s", r.URL)
			}
			reads++
			name, cursor := "node-a", "next"
			if r.URL.Query().Get("continue") == "next" {
				name, cursor = "node-b", ""
			}
			jsonResponse(t, w, map[string]any{"apiVersion": group + "/v1", "kind": "NodeMetricsList", "metadata": map[string]string{"continue": cursor}, "items": []any{metricNode(name, map[string]string{"memory": "1Mi", "cpu": "1m"})}})
		}), Options{MaxNodes: maximum})
		report, err := source.ListNodes(t.Context())
		if err != nil || reads != 2 {
			t.Fatalf("%+v %v reads=%d", report, err, reads)
		}
		if maximum == 1 && (report.Availability != Partial || report.Reason != LimitReached || len(report.Observations) != 1) {
			t.Fatalf("%+v", report)
		}
		if maximum == 5 && (report.Availability != Available || len(report.Observations) != 2) {
			t.Fatalf("%+v", report)
		}
	}
	source := newTestSource(t, func(http.ResponseWriter, *http.Request) { t.Error("unexpected request") }, Options{MaxNodes: 1})
	if report, err := source.ListNodes(t.Context()); err != nil || report.Availability != Forbidden {
		t.Fatalf("%+v %v", report, err)
	}
	if report, err := source.ReadNodes(t.Context(), []string{"a", "b"}); err != nil || report.Reason != LimitReached {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestNodeMetricsRejectInvalidIdentityAndRepeatedCursors(t *testing.T) {
	for _, mutation := range []string{"identity", "cursor"} {
		source := newClusterTestSource(t, nodeAPIHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			name := "node-a"
			if r.URL.Query().Get("continue") != "" {
				name = "node-b"
			}
			value := metricNode(name, map[string]string{"memory": "1Mi"})
			if mutation == "identity" {
				value["metadata"].(map[string]string)["namespace"] = "other-tenant"
			}
			jsonResponse(t, w, map[string]any{"apiVersion": group + "/v1", "kind": "NodeMetricsList", "metadata": map[string]string{"continue": "repeat"}, "items": []any{value}})
		}), Options{})
		report, err := source.ListNodes(t.Context())
		if err != nil || report.Availability != Unavailable || report.Reason != InvalidResponse || len(report.Observations) != 0 {
			t.Fatalf("%+v %v", report, err)
		}
	}
}
