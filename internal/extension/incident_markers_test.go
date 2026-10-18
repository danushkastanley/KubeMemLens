package extension

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestIncidentMarkersUseHistoryRevalidation(t *testing.T) {
	h, selection := contextReadFixture(t)
	source := sessionCaptureSource{reads: h}
	query, err := memoryhistory.ParseQuery(url.Values{"source": {"prometheus"}}, memoryhistory.Pod, h.now())
	if err != nil {
		t.Fatal(err)
	}
	p := incidentsession.Principal{Namespace: "team-a", NamespaceUID: "namespace-uid", Actor: "operator"}
	data, err := source.CaptureMarkers(context.Background(), p, "api", query)
	if err != nil {
		t.Fatal(err)
	}
	var bundle api.HistoryIncident
	if json.Unmarshal(data, &bundle) != nil || api.ValidateHistoryIncident(bundle) != nil || bundle.Context.History.Selection.UID != selection.UID || bundle.Context.Changes.Events != changemarkers.Missing {
		t.Fatal("canonical history/marker provenance lost")
	}
	for _, failure := range []error{memoryhistory.ErrDenied, memoryhistory.ErrChanged, memoryhistory.ErrBounds, changemarkers.ErrUnsupported} {
		h.memoryHistory.markers = markerProviderFixture{query: func(_ context.Context, s memoryhistory.Selection, q memoryhistory.Query) (changemarkers.Report, error) {
			return changemarkers.Compose(s, q, s.ResolvedAt, changemarkers.Missing, nil, false)
		}, revalidate: func(context.Context, changemarkers.Report) error { return failure }}
		data, err := source.CaptureMarkers(context.Background(), p, "api", query)
		if len(data) != 0 || !errors.Is(err, sessionHistoryError(failure)) {
			t.Fatal("marker revalidation failure disclosed evidence", err)
		}
	}
}

func TestIncidentMarkersRequireConfiguredScopeAndValidQuery(t *testing.T) {
	h, _ := contextReadFixture(t)
	source := sessionCaptureSource{reads: h}
	query, err := memoryhistory.ParseQuery(url.Values{"source": {"prometheus"}}, memoryhistory.Pod, h.now())
	if err != nil {
		t.Fatal(err)
	}
	p := incidentsession.Principal{Namespace: "team-b", NamespaceUID: "namespace-uid", Actor: "operator"}
	if _, err := source.CaptureMarkers(context.Background(), p, "api", query); !errors.Is(err, incidentsession.ErrDisabled) {
		t.Fatal("unconfigured namespace allowed")
	}
	p.Namespace = "team-a"
	invalid := query
	invalid.Source = "unknown"
	if _, err := source.CaptureMarkers(context.Background(), p, "api", invalid); !errors.Is(err, incidentsession.ErrInvalid) {
		t.Fatal("invalid source allowed")
	}
	h.memoryHistory.gate <- struct{}{}
	if _, err := source.CaptureMarkers(context.Background(), p, "api", query); !errors.Is(err, incidentsession.ErrCapacity) {
		t.Fatal("shared history admission bypassed")
	}
	<-h.memoryHistory.gate
	h.memoryHistory = nil
	if _, err := source.CaptureMarkers(context.Background(), p, "api", query); !errors.Is(err, incidentsession.ErrDisabled) {
		t.Fatal("disabled provider claimed evidence")
	}
}
