package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// hostileText is what a tenant can put in a Pod owner reference: an escape
// sequence, an OSC title change, a C1 control, a bidirectional override and a
// line separator.
const hostileText = "Foo\x1b[31mRED\x1b]0;title\x07\u009b2J‮gnp x"

func requireNoTerminalControl(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case string:
			if strings.ContainsFunc(typed, func(r rune) bool {
				return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
			}) {
				t.Fatalf("response text reaches the terminal with control characters: %q", typed)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for key, item := range typed {
				walk(key)
				walk(item)
			}
		}
	}
	walk(decoded)
}

func TestCollectorTextCannotCarryTerminalControlCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods":
			writeTestJSON(t, w, api.PodMemoryList{Items: []api.PodMemory{{Snapshot: api.PodSnapshot{
				Namespace: "team-a", PodName: "api",
				Context:    api.PodContext{OwnerKind: hostileText, OwnerName: hostileText, Labels: map[string]string{hostileText: hostileText}},
				Containers: []api.ContainerSnapshot{{ContainerName: hostileText, Context: api.ContainerContext{WorkloadKind: hostileText}}},
			}}}})
		case "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads":
			writeTestJSON(t, w, api.WorkloadMemoryList{Items: []api.WorkloadMemory{{Snapshot: api.WorkloadSnapshot{
				Namespace: "team-a", Kind: hostileText, Name: hostileText, LargestPodName: hostileText,
			}}}})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()
	scope, err := NamespaceScope("team-a")
	if err != nil {
		t.Fatal(err)
	}
	client := newTestKubernetesAPIClient(t, server.URL, scope)

	pods, err := client.Pods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	requireNoTerminalControl(t, pods)
	if got, want := pods[0].Context.OwnerKind, "Foo�[31mRED�]0;title��2J�gnp�x"; got != want {
		t.Fatalf("owner kind = %q, want every control character replaced and the rest kept: %q", got, want)
	}
	workloads, err := client.Workloads(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	requireNoTerminalControl(t, workloads)
}

func TestMetricsExpositionKeepsItsLineBreaks(t *testing.T) {
	const exposition = "# HELP a b\n# TYPE a gauge\na{pod=\"x\"}\t1\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		writeTestJSON(t, w, api.Metrics{ContentType: "text/plain", Content: exposition})
	}))
	defer server.Close()
	metrics, err := newTestKubernetesAPIClient(t, server.URL, AllNamespacesScope()).Metrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if metrics.Content != exposition {
		t.Fatalf("metrics content changed: %q", metrics.Content)
	}
}
