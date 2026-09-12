package kube

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVolumeHealthDiagnosticsAreAggregateAndCountPayloadChanges(t *testing.T) {
	r := &volumeResolver{}
	if r.VolumeHealthStats() != (VolumeHealthStats{}) {
		t.Fatal("disabled cache reported data")
	}
	r.health = newHealthCache()
	now := time.Now().UTC()
	o := cacheObservation(now)
	if _, err := r.health.observe(cacheKey(), o, now); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		now = now.Add(time.Second)
		o.ObservedAt = now
		if _, err := r.health.observe(cacheKey(), o, now); err != nil {
			t.Fatal(err)
		}
	}
	stats := r.VolumeHealthStats()
	if !stats.Enabled || stats.Entries != 1 || stats.Bytes <= 0 || stats.PayloadWrites != 1 {
		t.Fatal("unchanged status counted as payload writes", stats)
	}
	body, _ := json.Marshal(stats)
	for _, marker := range []string{"node-a", "node-uid", "fixture.csi.test", "private-backend-handle", "bounded-reason"} {
		if strings.Contains(string(body), marker) {
			t.Fatal("diagnostic identity leak")
		}
	}
}
