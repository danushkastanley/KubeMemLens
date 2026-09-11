package collector

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func nodeContextPage(keys []string, query url.Values, scope string) (PageSelection, error) {
	limit := nodecontext.MaxPageRecords
	if text := query.Get("limit"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed < 1 || parsed > limit {
			return PageSelection{}, fmt.Errorf("limit must be between 1 and %d", limit)
		}
		limit = parsed
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "continue": {query.Get("continue")}}
	return PaginateKeys(keys, values, scope)
}

func (s *Store) PageNodeContexts(now time.Time, query url.Values) ([]api.NodeContextRecord, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.expectedNodeUIDs))
	for name, uid := range s.expectedNodeUIDs {
		if uid != "" {
			keys = append(keys, name)
		}
	}
	page, err := nodeContextPage(keys, query, "nodecontexts:"+s.generation)
	if err != nil {
		return nil, "", err
	}
	items := make([]api.NodeContextRecord, len(page.Indexes))
	for offset, index := range page.Indexes {
		items[offset] = s.nodeContextRecordLocked(keys[index], now)
	}
	return items, page.Continue, nil
}

func (s *Store) GetNodeContext(name string, now time.Time) (api.NodeContextRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.expectedNodeUIDs[name] == "" {
		return api.NodeContextRecord{}, false
	}
	return s.nodeContextRecordLocked(name, now), true
}

func (s *Store) PageNodeContextHistory(name string, now time.Time, query url.Values, maxBytes int) (api.NodeContextHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeContext.prune(now)
	result := api.NodeContextHistory{NodeName: name, Generation: s.generation, ResetAt: s.startedAt,
		WindowSeconds: int64(nodecontext.HistoryDuration.Seconds()), Completeness: capability.Partial,
		CoverageLost: s.nodeContext.lossAt.After(now.Add(-nodecontext.HistoryDuration))}
	// A full elapsed window alone is not proof of continuous collection.
	keys := []string{}
	for uid, series := range s.nodeContext.history {
		if series.name == name {
			keys = append(keys, uid)
		}
	}
	if len(keys) == 1 && keys[0] == s.expectedNodeUIDs[name] && !result.CoverageLost &&
		!s.startedAt.After(now.Add(-nodecontext.HistoryDuration)) && completeNodeWindow(s.nodeContext.history[keys[0]], now) {
		result.Completeness = capability.Complete
	}

	page, err := nodeContextPage(keys, query, "nodecontext-history:"+name+":"+s.generation)
	if err != nil {
		return result, err
	}
	encodedBytes := 1024
	for _, index := range page.Indexes {
		for _, point := range s.nodeContext.history[keys[index]].points {
			encodedBytes += len(point.data) + 256
		}
	}
	if encodedBytes > maxBytes {
		return result, ErrReadPageTooLarge
	}

	result.Continue = page.Continue
	result.Series = make([]api.NodeContextHistorySeries, len(page.Indexes))
	for offset, index := range page.Indexes {
		series := s.nodeContext.history[keys[index]]
		points := make([]api.NodeContextHistoryPoint, len(series.points))
		for i, point := range series.points {
			points[i] = api.NodeContextHistoryPoint{ReceivedAt: point.at, Observation: *decodeNodeObservation(point.data)}
		}
		result.Series[offset] = api.NodeContextHistorySeries{NodeUID: series.uid, Points: points}
	}
	return result, nil
}

func (s *Store) NodeContextDebug(now time.Time) api.NodeContextDebug {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeContextDebugLocked(now)
}

func (s *Store) nodeContextDebugLocked(now time.Time) api.NodeContextDebug {
	store := s.nodeContext
	store.prune(now)
	result := api.NodeContextDebug{SharedNodeRecords: s.sharedNodeRecordsLocked(), Records: len(store.latest), MaxRecords: s.limits.MaxNodes,
		HistorySeries: len(store.history), HistoryBytes: store.historyBytes, MaxHistorySeries: nodecontext.MaxHistorySeries,
		MaxHistoryPoints: nodecontext.MaxHistoryPoints, MaxHistoryBytes: nodecontext.MaxHistoryBytes,
		CoverageLost: store.lossAt.After(now.Add(-nodecontext.HistoryDuration))}
	for _, series := range store.history {
		result.HistoryPoints += len(series.points)
	}
	for _, record := range store.latest {
		if record.failed {
			result.FailedRecords++
		}
		if record.capturedAt.IsZero() {
			continue
		}
		if now.Sub(record.capturedAt) > nodecontext.StaleAfter {
			result.StaleRecords++
		} else {
			result.FreshRecords++
		}
	}
	return result
}
