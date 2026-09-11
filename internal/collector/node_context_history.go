package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func (s *nodeContextStore) record(name, uid string, at time.Time, data []byte, now time.Time) {
	s.prune(now)
	for s.historyBytes+len(data) > nodecontext.MaxHistoryBytes {
		s.evictOldest(now)
	}
	series := s.history[uid]
	if series == nil {
		for len(s.history) >= nodecontext.MaxHistorySeries {
			s.evictOldestSeries(now)
		}
		series = &nodeContextSeries{name: name, uid: uid}
		s.history[uid] = series
	}
	if len(series.points) >= nodecontext.MaxHistoryPoints {
		s.historyBytes -= len(series.points[0].data)
		series.points[0] = nodeContextPoint{}
		series.points = series.points[1:]
		s.lossAt = now
	}
	series.points = append(series.points, nodeContextPoint{at: now, sourceAt: at, data: data})
	s.historyBytes += len(data)
}

func (s *nodeContextStore) prune(now time.Time) {
	cutoff := now.Add(-nodecontext.HistoryDuration)
	for uid, series := range s.history {
		for len(series.points) > 0 && series.points[0].at.Before(cutoff) {
			s.historyBytes -= len(series.points[0].data)
			series.points[0] = nodeContextPoint{}
			series.points = series.points[1:]
		}
		if len(series.points) == 0 {
			delete(s.history, uid)
		}
	}
}

func (s *nodeContextStore) evictOldest(now time.Time) {
	oldestUID := ""
	oldest := time.Time{}
	for uid, series := range s.history {
		at := series.points[0].at
		if oldestUID == "" || at.Before(oldest) || at.Equal(oldest) && uid < oldestUID {
			oldestUID, oldest = uid, at
		}
	}
	series := s.history[oldestUID]
	if series == nil {
		return
	}
	s.historyBytes -= len(series.points[0].data)
	series.points[0] = nodeContextPoint{}
	series.points = series.points[1:]
	if len(series.points) == 0 {
		delete(s.history, oldestUID)
	}
	s.lossAt = now
}

func (s *nodeContextStore) evictOldestSeries(now time.Time) {
	oldestUID := ""
	oldest := time.Time{}
	for uid, series := range s.history {
		at := series.points[len(series.points)-1].at
		if oldestUID == "" || at.Before(oldest) || at.Equal(oldest) && uid < oldestUID {
			oldestUID, oldest = uid, at
		}
	}
	if series := s.history[oldestUID]; series != nil {
		for _, point := range series.points {
			s.historyBytes -= len(point.data)
		}
		delete(s.history, oldestUID)
		s.lossAt = now
	}
}

func completeNodeWindow(series *nodeContextSeries, now time.Time) bool {
	if len(series.points) == 0 {
		return false
	}
	if now.Sub(series.points[len(series.points)-1].sourceAt) > nodecontext.StaleAfter {
		return false
	}
	tolerance := 2 * nodecontext.CollectionInterval
	if series.points[0].at.After(now.Add(-nodecontext.HistoryDuration+tolerance)) ||
		series.points[len(series.points)-1].at.Before(now.Add(-tolerance)) {
		return false
	}
	for i := 1; i < len(series.points); i++ {
		if series.points[i].at.Sub(series.points[i-1].at) > tolerance {
			return false
		}
	}
	return true
}
