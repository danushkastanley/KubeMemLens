package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type markerProviderFixture struct {
	query      func(context.Context, memoryhistory.Selection, memoryhistory.Query) (changemarkers.Report, error)
	revalidate func(context.Context, changemarkers.Report) error
}

func (p markerProviderFixture) Query(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (changemarkers.Report, error) {
	return p.query(ctx, s, q)
}
func (p markerProviderFixture) Revalidate(ctx context.Context, r changemarkers.Report) error {
	return p.revalidate(ctx, r)
}

func contextReadFixture(t *testing.T) (*ReadHandler, memoryhistory.Selection) {
	t.Helper()
	h, s := historyReadFixture(t)
	h.memoryHistory.contextResolver = h.memoryHistory.resolver
	h.memoryHistory.remote = historyProviderFunc(func(_ context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
		r, err := memoryhistory.NewReport(s, q, s.ResolvedAt)
		for i := range r.Series {
			r.Series[i].Origin = "cadvisor"
			r.Series[i].SampleClock = "prometheus-sample"
		}
		return r, err
	})
	h.memoryHistory.markers = markerProviderFixture{
		query: func(_ context.Context, s memoryhistory.Selection, q memoryhistory.Query) (changemarkers.Report, error) {
			return changemarkers.Compose(s, q, s.ResolvedAt, changemarkers.Missing, nil, false)
		},
		revalidate: func(context.Context, changemarkers.Report) error { return nil },
	}
	return h, s
}

const contextPath = "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends-context?source=prometheus"

func TestMemoryHistoryContextIsOptInAndPreservesHistory(t *testing.T) {
	h, _ := contextReadFixture(t)
	response := serveRead(t, h, contextPath)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var r memoryHistoryContextResource
	if err := json.Unmarshal(response.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.Kind != "MemoryHistoryContext" || r.Context.Validate() != nil || r.Context.Changes.Events != changemarkers.Missing {
		t.Fatal("context contract", r)
	}
	h.memoryHistory.markers = markerProviderFixture{query: func(context.Context, memoryhistory.Selection, memoryhistory.Query) (changemarkers.Report, error) {
		t.Fatal("plain history fetched markers")
		return changemarkers.Report{}, nil
	}}
	plain := serveRead(t, h, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus")
	if plain.Code != 200 {
		t.Fatal(plain.Code)
	}
	var old memoryHistoryResource
	if json.Unmarshal(plain.Body.Bytes(), &old) != nil || old.Kind != "MemoryHistory" || old.History.SchemaVersion != 1 {
		t.Fatal("existing history changed")
	}
	h.memoryHistory.markers = nil
	if got := serveRead(t, h, contextPath).Code; got != http.StatusNotFound {
		t.Fatal("disabled marker endpoint", got)
	}
}

func TestMemoryHistoryContextAuthorisationPrecedesProviderAndRepeats(t *testing.T) {
	for _, stage := range []string{"before acquisition", "history recheck", "marker recheck"} {
		t.Run(stage, func(t *testing.T) {
			h, s := contextReadFixture(t)
			calls := 0
			providerCalls := 0
			original := h.memoryHistory.remote
			h.memoryHistory.remote = historyProviderFunc(func(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (memoryhistory.Report, error) {
				providerCalls++
				return original.Query(ctx, s, q)
			})
			h.memoryHistory.contextResolver = historyResolveFunc(func(context.Context, memoryhistory.Request) (memoryhistory.Selection, error) {
				calls++
				if stage == "before acquisition" || (stage == "history recheck" && calls == 2) {
					return memoryhistory.Selection{}, memoryhistory.ErrDenied
				}
				return s, nil
			})
			if stage == "marker recheck" {
				p := h.memoryHistory.markers.(markerProviderFixture)
				p.revalidate = func(context.Context, changemarkers.Report) error { return memoryhistory.ErrDenied }
				h.memoryHistory.markers = p
			}
			response := serveRead(t, h, contextPath)
			if response.Code != http.StatusForbidden {
				t.Fatal(response.Code, response.Body.String())
			}
			if stage == "before acquisition" && providerCalls != 0 {
				t.Fatal("denied context triggered history acquisition")
			}
		})
	}
}

func TestMemoryHistoryContextRejectsChangedAndMismatchedEvidence(t *testing.T) {
	for _, change := range []string{"marker UID", "memory UID", "query window", "Node"} {
		t.Run(change, func(t *testing.T) {
			h, _ := contextReadFixture(t)
			p := h.memoryHistory.markers.(markerProviderFixture)
			original := p.query
			p.query = func(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (changemarkers.Report, error) {
				r, err := original(ctx, s, q)
				switch change {
				case "marker UID":
					r.UID = "replacement"
				case "query window":
					r.Start = r.End
				case "memory UID":
					r.Request.Name = "other"
				}
				return r, err
			}
			h.memoryHistory.markers = p
			path := contextPath
			if change == "Node" {
				path = "/apis/memory.kubememlens.io/v1alpha1/nodes/node-a/trends-context?source=prometheus"
			}
			if response := serveRead(t, h, path); response.Code == 200 {
				t.Fatal("mismatched source evidence disclosed")
			}
		})
	}
}

func TestMemoryHistoryContextKeepsMarkerBudgetErrorsDistinct(t *testing.T) {
	h, _ := contextReadFixture(t)
	p := h.memoryHistory.markers.(markerProviderFixture)
	original := p.query
	p.query = func(ctx context.Context, s memoryhistory.Selection, q memoryhistory.Query) (changemarkers.Report, error) {
		r, err := original(ctx, s, q)
		r.Markers = make([]changemarkers.Marker, changemarkers.MaxMarkers+1)
		return r, err
	}
	h.memoryHistory.markers = p
	if response := serveRead(t, h, contextPath); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("marker budget failure misclassified", response.Code, response.Body.String())
	}
}

func TestMemoryHistoryContextReportsUnsupportedOwnersExplicitly(t *testing.T) {
	h, _ := contextReadFixture(t)
	p := h.memoryHistory.markers.(markerProviderFixture)
	p.query = func(context.Context, memoryhistory.Selection, memoryhistory.Query) (changemarkers.Report, error) {
		return changemarkers.Report{}, changemarkers.ErrUnsupported
	}
	h.memoryHistory.markers = p
	if response := serveRead(t, h, contextPath); response.Code != http.StatusUnprocessableEntity {
		t.Fatal(response.Code, response.Body.String())
	}
}
