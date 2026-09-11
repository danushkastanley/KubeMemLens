package client

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

type nodeEvidenceFixture struct {
	record   api.NodeContextResource
	analysis nodeanalysis.Analysis
	order    []string
	err      error
}

func (f *nodeEvidenceFixture) NodeContext(context.Context, string) (api.NodeContextResource, error) {
	f.order = append(f.order, "record")
	return f.record, nil
}
func (f *nodeEvidenceFixture) NodeAnalysis(context.Context, string, nodeanalysis.Metric, int) (nodeanalysis.Analysis, error) {
	f.order = append(f.order, "analysis")
	return f.analysis, f.err
}

func TestReadNodeEvidenceBindsSourceAndReadsAuthorisationLast(t *testing.T) {
	makeFixture := func() *nodeEvidenceFixture {
		memory := &nodecontext.Memory{CapturedAt: time.Now().UTC()}
		return &nodeEvidenceFixture{record: api.NodeContextResource{Record: api.NodeContextRecord{NodeName: "node-a", NodeUID: "uid", LastGood: &nodecontext.Observation{Stats: &nodecontext.Stats{Memory: memory}}}}, analysis: nodeanalysis.Analysis{NodeName: "node-a", NodeUID: "uid", Facts: nodeanalysis.Facts{Memory: memory, Availability: capability.Unreported}}}
	}
	f := makeFixture()
	if _, err := ReadNodeEvidence(t.Context(), f, "node-a", nodeanalysis.Total, 20); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(f.order) != "[record analysis]" {
		t.Fatal("authorisation was not the final read", f.order)
	}
	for name, change := range map[string]func(*nodeEvidenceFixture){
		"replacement": func(f *nodeEvidenceFixture) { f.analysis.NodeUID = "new" },
		"sample changed": func(f *nodeEvidenceFixture) {
			f.analysis.Facts.Memory = &nodecontext.Memory{CapturedAt: time.Now().Add(time.Second)}
		},
		"source failed": func(f *nodeEvidenceFixture) { f.analysis.Facts.Availability = capability.Forbidden },
		"revoked":       func(f *nodeEvidenceFixture) { f.err = &ReadError{Kind: ReadErrorForbidden} },
	} {
		t.Run(name, func(t *testing.T) {
			f := makeFixture()
			change(f)
			got, err := ReadNodeEvidence(t.Context(), f, "node-a", nodeanalysis.Total, 20)
			if err == nil || got.Record.NodeUID != "" || got.Analysis.NodeUID != "" {
				t.Fatal("inconsistent or unauthorised evidence returned", got, err)
			}
		})
	}
}

type nodeHistoryFixture func(context.Context, string, string) (api.NodeContextHistory, error)

func (f nodeHistoryFixture) NodeContextHistory(c context.Context, n, k string) (api.NodeContextHistory, error) {
	return f(c, n, k)
}

func TestReadNodeHistoryBoundsAndFailureAtomicity(t *testing.T) {
	for _, scenario := range []string{"success", "denied", "repeated token", "repeated uid", "generation changed", "too many", "wrong node", "wrong point", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := nodeHistoryFixture(func(_ context.Context, name, token string) (api.NodeContextHistory, error) {
				calls++
				p := api.NodeContextHistory{NodeName: name, Generation: "generation", Completeness: capability.Complete, Series: []api.NodeContextHistorySeries{{NodeUID: fmt.Sprint(calls)}}}
				if calls == 1 || scenario == "too many" {
					p.Continue = fmt.Sprint(calls)
				}
				if calls == 1 && scenario == "cancelled" {
					cancel()
				}
				if calls == 2 {
					switch scenario {
					case "denied":
						return api.NodeContextHistory{}, &ReadError{Kind: ReadErrorForbidden}
					case "repeated token":
						p.Continue = token
					case "repeated uid":
						p.Series[0].NodeUID = "1"
					case "generation changed":
						p.Generation = "new"
					case "wrong node":
						p.NodeName = "other"
					case "wrong point":
						p.Series[0].Points = []api.NodeContextHistoryPoint{{Observation: nodecontext.Observation{NodeName: name, NodeUID: "other"}}}
					}
				}
				return p, nil
			})
			result, err := ReadNodeHistory(ctx, reader, "node-a")
			if scenario == "success" {
				if err != nil || len(result.Series) != 2 || result.Continue != "" || result.Completeness != capability.Partial {
					t.Fatal(result, err)
				}
				return
			}
			if err == nil || len(result.Series) != 0 {
				t.Fatal("failed traversal returned history", result, err)
			}
			if calls > MaxNodeHistoryInstances {
				t.Fatal("unbounded traversal", calls)
			}
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
