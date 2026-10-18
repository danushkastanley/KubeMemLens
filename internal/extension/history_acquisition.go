package extension

import (
	"context"
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type historyAcquisition struct {
	Selection memoryhistory.Selection
	History   memoryhistory.Report
	Changes   changemarkers.Report
}

// acquire uses the same source and lifetime revalidation for live history reads
// and retained incident evidence. Callers own scope, query and admission limits.
func (s *memoryHistoryService) acquire(ctx context.Context, request memoryhistory.Request, query memoryhistory.Query, resolver memoryhistory.Resolver, markers changemarkers.Provider) (historyAcquisition, error) {
	if resolver == nil {
		return historyAcquisition{}, memoryhistory.ErrUnavailable
	}
	selected, err := resolver.Resolve(ctx, request)
	if err != nil {
		return historyAcquisition{}, err
	}
	if selected.Request != request || selected.Validate() != nil {
		return historyAcquisition{}, memoryhistory.ErrInvalid
	}
	provider := s.local
	if query.Source == memoryhistory.Prometheus {
		provider = s.remote
	}
	if provider == nil {
		return historyAcquisition{}, memoryhistory.ErrUnavailable
	}
	report, err := provider.Query(ctx, selected, query)
	if err != nil {
		return historyAcquisition{}, err
	}
	var changes changemarkers.Report
	if markers != nil {
		changes, err = markers.Query(ctx, selected, query)
		if err != nil {
			return historyAcquisition{}, err
		}
	}
	current, err := resolver.Resolve(ctx, request)
	if err != nil {
		return historyAcquisition{}, err
	}
	if current.Request != selected.Request || current.UID != selected.UID || current.Revision != selected.Revision || !slices.Equal(current.Targets, selected.Targets) {
		return historyAcquisition{}, memoryhistory.ErrChanged
	}
	if markers != nil {
		if err := markers.Revalidate(ctx, changes); err != nil {
			return historyAcquisition{}, err
		}
	}
	if ctx.Err() != nil {
		return historyAcquisition{}, ctx.Err()
	}
	return historyAcquisition{Selection: selected, History: report, Changes: changes}, nil
}
