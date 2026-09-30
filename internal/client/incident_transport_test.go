package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"k8s.io/client-go/rest"
)

func TestIncidentClientDoesNotRetryUncertainMutation(t *testing.T) {
	var calls atomic.Int32
	client, _ := incidentTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get(incidentsession.SchemaHeader) != "1" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("missing protocol controls")
		}
		w.Header().Set(incidentsession.SchemaHeader, "1")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		fmt.Fprint(w, `{"private":"malformed acknowledgement"}`)
	}))
	_, err := client.StartIncident(context.Background())
	var failure *IncidentError
	if !errors.As(err, &failure) || !failure.OutcomeUnknown || calls.Load() != 1 {
		t.Fatal("uncertain mutation retried or misreported", err)
	}
	if strings.Contains(fmt.Sprintf("%#v", failure), "private") {
		t.Fatal("private response exposed in error")
	}
}

func TestIncidentClientRejectsRedirectAndOversizedExport(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.invalid/private", 302)
		}},
		{"oversized", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(incidentsession.SchemaHeader, "1")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, strings.Repeat(" ", incidentsession.MaxExportBytes+1))
		}},
		{"wrong version", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(incidentsession.SchemaHeader, "2")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "{}")
		}},
		{"compressed", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(incidentsession.SchemaHeader, "1")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			fmt.Fprint(w, "private")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, _ := incidentTLSClient(t, test.handler)
			body, err := client.ExportIncident(context.Background(), strings.Repeat("a", 32), incidentsession.ExportSanitised)
			if test.name == "redirect" {
				var failure *IncidentError
				if !errors.As(err, &failure) || failure.StatusCode != 302 {
					t.Fatal("redirect was not rejected at the first response")
				}
			}
			if err == nil || len(body) != 0 {
				t.Fatal("invalid response returned")
			}
		})
	}
}

func TestIncidentClientRejectsUnsafeScopeAndTransport(t *testing.T) {
	scope, _ := NamespaceScope("team-a")
	for _, config := range []*rest.Config{nil, {Host: "http://example.invalid"}, {Host: "https://example.invalid", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}, {Host: "https://user:secret@example.invalid"}, {Host: "https://example.invalid?private=query"}} {
		if _, err := NewIncidentSessionClient(config, scope); err == nil {
			t.Fatal("unsafe incident transport accepted")
		}
	}
	if _, err := NewIncidentSessionClient(&rest.Config{Host: "https://example.invalid"}, AllNamespacesScope()); err == nil {
		t.Fatal("unscoped session client accepted")
	}
	var calls atomic.Int32
	client, _ := incidentTLSClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	for _, id := range []string{"", "../escape", strings.Repeat("A", 32)} {
		if _, err := client.InspectIncident(context.Background(), id); err == nil {
			t.Fatal("invalid ID accepted")
		}
		if _, err := client.AnnotateIncident(context.Background(), id, "private note"); err == nil {
			t.Fatal("invalid mutation ID accepted")
		}
	}
	for _, note := range []string{string([]byte{0xff}), "first\nsecond", strings.Repeat("x", 513)} {
		if _, err := client.AnnotateIncident(context.Background(), strings.Repeat("a", 32), note); err == nil {
			t.Fatal("invalid annotation encoded or sent")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.StartIncident(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancelled request issued")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid operation reached network")
	}
}
