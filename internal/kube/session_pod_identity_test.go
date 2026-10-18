package kube

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const sessionPod = `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"api","namespace":"team-a","uid":"uid-a"},"spec":{"nodeName":"node-a"}}`

func TestSessionPodIdentityBoundToRequestedObject(t *testing.T) {
	reader, _ := namespaceServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/team-a/pods/api" || r.Method != "GET" {
			t.Error("unexpected identity request")
		}
		fmt.Fprint(w, sessionPod)
	})
	got, err := reader.LookupPod(context.Background(), "team-a", "api")
	if err != nil || got.UID != "uid-a" || got.NodeName != "node-a" {
		t.Fatal("live Pod identity missing", err)
	}
	for _, pair := range [][2]string{{"../team-a", "api"}, {"team-a", "../api"}, {"team-a", ""}} {
		if _, err := reader.LookupPod(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatal("invalid lookup accepted")
		}
	}
}

func TestSessionPodIdentityRejectsMismatchedEvidence(t *testing.T) {
	for _, body := range []string{
		strings.Replace(sessionPod, `"team-a"`, `"team-b"`, 1),
		strings.Replace(sessionPod, `"name":"api"`, `"name":"other"`, 1),
		strings.Replace(sessionPod, `"uid-a"`, `""`, 1),
		strings.Replace(sessionPod, `"node-a"`, `""`, 1),
		strings.Replace(sessionPod, `"Pod"`, `"Namespace"`, 1),
		strings.Replace(sessionPod, `"v1"`, `"v2"`, 1),
		`{`, strings.Repeat(" ", maxHealthResponse+1),
	} {
		reader, _ := namespaceServer(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) })
		if got, err := reader.LookupPod(context.Background(), "team-a", "api"); err == nil || got != (SessionPodIdentity{}) {
			t.Fatal("mismatched Pod identity accepted")
		}
	}
}
