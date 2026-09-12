package collector

import (
	"testing"
	"time"
)

func TestVolumeDiagnosticsCountAcceptedRetentionOnly(t *testing.T) {
	now := time.Now().UTC()
	s := NewStore()
	s.now = func() time.Time { return now }
	if err := s.ReconcileNodeIdentities(map[string]string{"node-a": "uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	if s.VolumeUsageStats() != (VolumeUsageStats{}) {
		t.Fatal("initial retention is not empty")
	}
	if err := s.ReplaceNodeContextWithVolumes(contextSample(now), volumeBody(t, now, "uid-a", 10)); err != nil {
		t.Fatal(err)
	}
	before := s.VolumeUsageStats()
	if before.Entries != 1 || before.Bytes <= 0 || before.Commits != 1 {
		t.Fatal("accepted entry not counted", before)
	}
	if _, err := s.VolumeSamples(volumeScope(now), now); err != nil {
		t.Fatal(err)
	}
	if s.VolumeUsageStats() != before {
		t.Fatal("read changed retention counters")
	}
	now = now.Add(time.Second)
	if err := s.ReplaceNodeContextWithVolumes(contextSample(now), []byte("{")); err == nil {
		t.Fatal("invalid input accepted")
	}
	if s.VolumeUsageStats() != before {
		t.Fatal("rejected entry changed counters")
	}
	if err := s.ReplaceNodeContextWithVolumes(contextSample(now), volumeBody(t, now, "uid-a", 20)); err != nil {
		t.Fatal(err)
	}
	if got := s.VolumeUsageStats(); got.Entries != 1 || got.Commits != 2 {
		t.Fatal("replacement counter", got)
	}
	now = now.Add(time.Second)
	if err := s.ReplaceNodeContextWithVolumes(contextSample(now), nil); err != nil {
		t.Fatal(err)
	}
	if got := s.VolumeUsageStats(); got.Entries != 0 || got.Bytes != 0 || got.Commits != 2 {
		t.Fatal("disabled state retained data or reset counters", got)
	}
}
