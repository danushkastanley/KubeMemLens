package promhistory

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestMaximumProfileReturnsAllBoundedSeries(t *testing.T) {
	s := fixtureSelection()
	prototype := s.Targets[0]
	s.Targets = nil
	prototype.StartedAt = s.ResolvedAt.Add(-48 * time.Hour)
	for i := 0; i < memoryhistory.MaxTargets; i++ {
		target := prototype
		target.Container = fmt.Sprintf("app-%02d", i)
		target.ContainerID = "containerd://" + target.Container
		s.Targets = append(s.Targets, target)
	}
	q := fixtureQuery(s)
	q.Start = q.End.Add(-24 * time.Hour)
	q.Step = 6 * time.Minute
	response := fixtureResponse(s, q)
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
	for range 3 {
		r, err := c.Query(t.Context(), s, q)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		if len(r.Series) != memoryhistory.MaxTargets || len(r.Series[0].Points) != memoryhistory.MaxPoints || r.Completeness != memoryhistory.Complete {
			t.Fatal("maximum profile truncated")
		}
		data, err := json.Marshal(r)
		if err != nil || len(data) > memoryhistory.MaxResponseBytes {
			t.Fatal("accepted profile exceeds API byte budget")
		}
	}
}

func TestGaugeDecreaseIsNotConvertedIntoACounterReset(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	response := fixtureResponse(s, q)
	response.Data.Result[0].Values[0][1] = json.RawMessage(`"100"`)
	response.Data.Result[0].Values[1][1] = json.RawMessage(`"20"`)
	response.Data.Result[0].Values[2][1] = json.RawMessage(`"0"`)
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
	r, err := c.Query(t.Context(), s, q)
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []uint64{100, 20, 0} {
		if r.Series[0].Points[i].Bytes == nil || *r.Series[0].Points[i].Bytes != value {
			t.Fatal("gauge decrease changed value or availability")
		}
	}
}
