package collector

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

var ErrNodeIdentityUnavailable = errors.New("current Node identity is unavailable or does not match")

// Encoded records are immutable, bound retained bytes, and prevent caller
// mutation through pointer-rich observations. History shares accepted bytes.
type nodeContextEntry struct {
	uid        string
	report     []byte
	good       []byte
	reportedAt time.Time
	capturedAt time.Time
	receivedAt time.Time
	failed     bool
	clock      nodeContextClock
}

type nodeContextStore struct {
	latest       map[string]nodeContextEntry
	history      map[string]*nodeContextSeries
	historyBytes int
	lossAt       time.Time
}

type nodeContextSeries struct {
	name   string
	uid    string
	points []nodeContextPoint
}

type nodeContextPoint struct {
	sourceAt time.Time
	at       time.Time
	data     []byte
}

func newNodeContextStore() *nodeContextStore {
	return &nodeContextStore{latest: map[string]nodeContextEntry{}, history: map[string]*nodeContextSeries{}}
}

// ReplaceNodeContext requires a fresh, collector-observed Node UID inventory.
// It never changes container ownership, snapshots, counters or history.
func (s *Store) ReplaceNodeContext(value nodecontext.Observation) error {
	return s.ReplaceNodeContextWithVolumes(value, nil)
}

func (s *Store) ReplaceNodeContextWithVolumes(value nodecontext.Observation, volumes []byte) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > nodecontext.MaxObservationBytes {
		return nodecontext.ErrInvalidObservation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !s.nodeIdentityCurrentLocked(value.NodeName, value.NodeUID, now) {
		return ErrNodeIdentityUnavailable
	}
	if err := nodecontext.Validate(value, now, 2*time.Minute, 30*time.Second); err != nil {
		return err
	}
	store := s.nodeContext
	previous, exists := store.latest[value.NodeName]
	if !exists && s.nodeRecordCountLocked(value.NodeName) >= s.limits.MaxNodes {
		return ErrStoreCapacity
	}
	if previous.uid != "" && previous.uid != value.NodeUID {
		previous = nodeContextEntry{}
	}
	if !value.ReportedAt.After(previous.reportedAt) {
		return ErrSnapshotOutOfOrder
	}
	clock := observationClock(value)
	if clock.before(previous.clock) {
		return ErrSnapshotOutOfOrder
	}
	volumeEntry, err := s.prepareVolumeEntryLocked(value, volumes, now)
	if err != nil {
		return err
	}
	entry := nodeContextEntry{clock: clock.retainingMissing(previous.clock), uid: value.NodeUID, report: data, good: previous.good,
		reportedAt: value.ReportedAt, capturedAt: previous.capturedAt, receivedAt: now, failed: value.Availability != capability.Available}
	if value.Stats != nil {
		entry.good = data
		entry.capturedAt = value.Evidence.CapturedAt
		if clock.newMeasurements(previous.clock) {
			store.record(value.NodeName, value.NodeUID, entry.capturedAt, data, now)
		}
	}
	store.latest[value.NodeName] = entry
	s.commitVolumeEntryLocked(value.NodeName, volumeEntry)
	return nil
}

func (s *Store) nodeIdentityCurrentLocked(name, uid string, now time.Time) bool {
	return uid != "" && s.expectedNodeUIDs[name] == uid && s.inventoryKnown &&
		!s.inventoryUpdatedAt.After(now) && now.Sub(s.inventoryUpdatedAt) <= nodecontext.StaleAfter
}

func (s *Store) nodeRecordCountLocked(name string) int {
	// A name already owned by either producer does not consume another Node slot.
	if _, exists := s.nodes[name]; exists {
		return 0
	}
	if _, exists := s.nodeContext.latest[name]; exists {
		return 0
	}
	return s.sharedNodeRecordsLocked()
}

func (s *Store) sharedNodeRecordsLocked() int {
	count := len(s.nodes)
	for node := range s.nodeContext.latest {
		if _, exists := s.nodes[node]; !exists {
			count++
		}
	}
	return count
}

func decodeNodeObservation(data []byte) *nodecontext.Observation {
	if len(data) == 0 {
		return nil
	}
	var value nodecontext.Observation
	// Only previously validated, internally encoded bytes reach this function.
	if err := json.Unmarshal(data, &value); err != nil {
		panic("corrupt internal Node observation")
	}
	return &value
}

func (s *Store) nodeContextRecordLocked(name string, now time.Time) api.NodeContextRecord {
	entry, exists := s.nodeContext.latest[name]
	record := api.NodeContextRecord{NodeName: name, NodeUID: s.expectedNodeUIDs[name], Freshness: capability.Rebuilding}
	if !exists {
		return record
	}
	record.NodeUID, record.ReceivedAt = entry.uid, entry.receivedAt
	record.Report, record.LastGood = decodeNodeObservation(entry.report), decodeNodeObservation(entry.good)
	record.Freshness = capability.UnknownFreshness
	if record.LastGood != nil {
		record.Freshness = capability.Fresh
		if now.Sub(entry.capturedAt) > nodecontext.StaleAfter {
			record.Freshness = capability.Stale
		}
		record.LastGood.Evidence.Freshness = record.Freshness
		if record.Report.Stats != nil {
			record.Report.Evidence.Freshness = record.Freshness
		}
	}
	return record
}

// EnableNodeContext is called during authenticated server configuration only.
func (s *Store) EnableNodeContext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodeContextEnabled = true
}
