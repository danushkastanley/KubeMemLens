package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

type volumeEntry struct {
	uid        string
	reportedAt time.Time
	state      volumecontext.Usage
	current    []byte
	good       []byte
}

type volumeStore struct {
	entries        map[string]volumeEntry
	bytes          int
	healthReserved bool
	commits        uint64
}

func newVolumeStore() *volumeStore { return &volumeStore{entries: map[string]volumeEntry{}} }
func (e volumeEntry) bytes() int   { return len(e.current) + len(e.good) + len(e.uid) + 1024 }

// prepareVolumeEntryLocked validates before either Node or volume state changes.
func (s *Store) prepareVolumeEntryLocked(node nodecontext.Observation, raw []byte, now time.Time) (volumeEntry, error) {
	prior := s.volumes.entries[node.NodeName]
	if prior.uid != node.NodeUID {
		prior = volumeEntry{}
	}
	state := volumecontext.SourceState(volumehealth.Disabled, volumecontext.Disabled)
	if len(raw) == 0 && prior.uid == "" {
		return volumeEntry{state: state}, nil
	}
	var batch *volumecontext.Batch
	if len(raw) > 0 {
		decoded, err := volumecontext.DecodePrivate(raw, now)
		if err != nil {
			return volumeEntry{}, err
		}
		name, uid := decoded.NodeIdentity()
		if name != node.NodeName || uid != node.NodeUID || !decoded.ReportedAt().Equal(node.ReportedAt) {
			return volumeEntry{}, volumecontext.ErrScope
		}
		batch = &decoded
		state = decoded.State()
	} else if node.Availability == capability.Unavailable {
		state = volumecontext.SourceState(volumehealth.Unavailable, volumecontext.SourceFailed)
	} else if node.Availability == capability.Forbidden {
		state = volumecontext.SourceState(volumehealth.Forbidden, volumecontext.AccessDenied)
	} else if node.Availability == capability.Unsupported {
		state = volumecontext.SourceState(volumehealth.Unsupported, volumecontext.Unsupported)
	}
	entry := volumeEntry{uid: node.NodeUID, reportedAt: node.ReportedAt, state: state}
	if batch != nil {
		body, err := batch.EncodePrivate()
		if err != nil {
			return volumeEntry{}, err
		}
		entry.current = body
	}
	if state.Availability == volumehealth.Reported || state.Availability == volumehealth.Unreported || state.Availability == volumehealth.Unavailable {
		good, err := retainVolumeEntry(prior, batch, node, now)
		if err != nil {
			return volumeEntry{}, err
		}
		entry.good = good
	}
	oldBytes := 0
	if previous, exists := s.volumes.entries[node.NodeName]; exists {
		oldBytes = previous.bytes()
	}
	if s.volumes.bytes-oldBytes+entry.bytes() > s.volumeUsageLimitLocked() {
		return volumeEntry{}, ErrStoreCapacity
	}
	return entry, nil
}

// ReserveVolumeHealth keeps usage plus sanitised health within the shared
// retention ceiling. Configuration happens before the server accepts traffic.
func (s *Store) ReserveVolumeHealth() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.volumes.bytes > volumecontext.MaxRetainedBytes-volumecontext.MaxHealthBytes {
		return ErrStoreCapacity
	}
	s.volumes.healthReserved = true
	return nil
}

func (s *Store) volumeUsageLimitLocked() int {
	if s.volumes.healthReserved {
		return volumecontext.MaxRetainedBytes - volumecontext.MaxHealthBytes
	}
	return volumecontext.MaxRetainedBytes
}

func retainVolumeEntry(prior volumeEntry, current *volumecontext.Batch, node nodecontext.Observation, now time.Time) ([]byte, error) {
	var previous *volumecontext.Batch
	if len(prior.good) > 0 {
		decoded, err := volumecontext.DecodePrivate(prior.good, prior.reportedAt)
		if err != nil {
			return nil, err
		}
		previous = &decoded
	}
	if current == nil {
		empty, err := volumecontext.NewBatch(node.NodeName, node.NodeUID, node.ReportedAt, volumecontext.SourceState(volumehealth.Unavailable, volumecontext.SourceFailed), nil, now)
		if err != nil {
			return nil, err
		}
		current = &empty
	}
	retained, err := volumecontext.Retain(previous, *current, now)
	if err != nil {
		return nil, err
	}
	return retained.EncodePrivate()
}

func (s *Store) commitVolumeEntryLocked(name string, entry volumeEntry) {
	s.deleteVolumeEntryLocked(name)
	// Disabled profiles keep no volume state or identity index.
	if entry.state.Availability == volumehealth.Disabled {
		return
	}
	s.volumes.entries[name] = entry
	s.volumes.bytes += entry.bytes()
	s.volumes.commits++
}

func (s *Store) deleteVolumeEntryLocked(name string) {
	if old, exists := s.volumes.entries[name]; exists {
		s.volumes.bytes -= old.bytes()
		delete(s.volumes.entries, name)
	}
}

// VolumeSamples performs no access decision. The production read handler must
// supply a live authorised Pod scope before calling JoinSamples.
func (s *Store) VolumeSamples(scope volumecontext.PodScope, now time.Time) (volumecontext.Samples, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := volumecontext.Samples{State: volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport)}
	if !s.nodeIdentityCurrentLocked(scope.NodeName, scope.NodeUID, now) {
		return result, ErrNodeIdentityUnavailable
	}
	entry, exists := s.volumes.entries[scope.NodeName]
	if !exists {
		return result, nil
	}
	if entry.uid != scope.NodeUID {
		return result, ErrNodeIdentityUnavailable
	}
	result.State = entry.state
	for _, item := range []struct {
		data   []byte
		target *[]volumecontext.RawUsage
	}{{entry.current, &result.Current}, {entry.good, &result.LastGood}} {
		if len(item.data) == 0 {
			continue
		}
		batch, err := volumecontext.DecodePrivate(item.data, entry.reportedAt)
		if err != nil {
			return volumecontext.Samples{}, err
		}
		*item.target = batch.ForPod(scope)
	}
	return result, nil
}

func (s *Store) pruneVolumeEntriesLocked(now time.Time) {
	for name, entry := range s.volumes.entries {
		if now.Sub(entry.reportedAt) > volumecontext.ExpireAfter {
			s.deleteVolumeEntryLocked(name)
		}
	}
}

// VolumeNodeUID is internal binding information, not a Node disclosure API.
func (s *Store) VolumeNodeUID(name string, now time.Time) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	uid := s.expectedNodeUIDs[name]
	return uid, s.nodeIdentityCurrentLocked(name, uid, now)
}
