package client

import (
	"context"
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

const MaxNodeHistoryInstances = 8

type NodeEvidenceReader interface {
	NodeContext(context.Context, string) (api.NodeContextResource, error)
	NodeAnalysis(context.Context, string, nodeanalysis.Metric, int) (nodeanalysis.Analysis, error)
}

type NodeHistoryReader interface {
	NodeContextHistory(context.Context, string, string) (api.NodeContextHistory, error)
}

func ReadNodeEvidence(ctx context.Context, reader NodeEvidenceReader, name string, rank nodeanalysis.Metric, limit int) (api.NodeEvidence, error) {
	record, err := reader.NodeContext(ctx, name)
	if err != nil {
		return api.NodeEvidence{}, err
	}
	// Read analysis last: contributor authorisation must be evaluated anew for
	// this operation, including capture, rather than inherited from a screen.
	analysis, err := reader.NodeAnalysis(ctx, name, rank, limit)
	if err != nil {
		return api.NodeEvidence{}, err
	}
	if record.Record.NodeName != name || !(api.NodeEvidence{Record: record.Record, Analysis: analysis}).Consistent() {
		return api.NodeEvidence{}, fmt.Errorf("Node evidence changed during the read; refresh and try again")
	}
	return api.NodeEvidence{Record: record.Record, Analysis: analysis}, nil
}

// ReadNodeHistory bounds total traversal as well as individual server pages.
// Any failed page discards this read; callers may retain a labelled last-good
// Node-only history separately. Partial reads are never exported as complete.
func ReadNodeHistory(ctx context.Context, reader NodeHistoryReader, name string) (api.NodeContextHistory, error) {
	var result api.NodeContextHistory
	token := ""
	seenTokens := map[string]bool{}
	seenUIDs := map[string]bool{}
	for pageIndex := 0; pageIndex < MaxNodeHistoryInstances; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return api.NodeContextHistory{}, err
		}
		page, err := reader.NodeContextHistory(ctx, name, token)
		if err != nil {
			return api.NodeContextHistory{}, err
		}
		if page.NodeName != name || len(page.Series) > 1 || page.Generation == "" ||
			(page.Completeness != capability.Complete && page.Completeness != capability.Partial) {
			return api.NodeContextHistory{}, fmt.Errorf("invalid Node history page")
		}
		if pageIndex == 0 {
			result = page
			result.Series = nil
		} else if page.Generation != result.Generation || !page.ResetAt.Equal(result.ResetAt) || page.WindowSeconds != result.WindowSeconds {
			return api.NodeContextHistory{}, fmt.Errorf("Node history restarted during pagination; refresh and try again")
		}
		result.CoverageLost = result.CoverageLost || page.CoverageLost
		if page.Completeness != capability.Complete {
			result.Completeness = capability.Partial
		}
		for _, series := range page.Series {
			if series.NodeUID == "" || seenUIDs[series.NodeUID] || len(series.Points) > nodecontext.MaxHistoryPoints {
				return api.NodeContextHistory{}, fmt.Errorf("invalid Node history instance or point bounds")
			}
			for _, point := range series.Points {
				if point.Observation.NodeName != name || point.Observation.NodeUID != series.NodeUID {
					return api.NodeContextHistory{}, fmt.Errorf("Node history contains mismatched identity")
				}
			}
			seenUIDs[series.NodeUID] = true
			result.Series = append(result.Series, series)
		}
		result.Continue = ""
		if page.Continue == "" {
			if result.CoverageLost || len(result.Series) > 1 {
				result.Completeness = capability.Partial
			}
			return result, nil
		}
		if len(page.Continue) > 4096 || seenTokens[page.Continue] {
			return api.NodeContextHistory{}, fmt.Errorf("invalid Node history continuation")
		}
		seenTokens[page.Continue] = true
		token = page.Continue
	}
	return api.NodeContextHistory{}, fmt.Errorf("Node history exceeds %d instances; retry after older instances expire", MaxNodeHistoryInstances)
}
