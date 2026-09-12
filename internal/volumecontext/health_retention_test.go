package volumecontext

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestHealthFailureKeepsHistoricalAdverseSeparate(t *testing.T) {
	s, b, u := fixture()
	prior := healthFixture(volumehealth.BackendSource, "StorageDegraded")
	current := prior
	current.Availability, current.Reason = volumehealth.Unavailable, volumehealth.ReadFailed
	current.Conditions = nil
	current.ObservedAt = testNow.Add(time.Second)
	current.LastGood = &prior.Observation
	r, err := Join(s, b, u, []HealthObservation{current}, SourceState(volumehealth.Reported, ""), current.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	h := r.Authorised().Volumes[0].Health[0]
	if h.Observation.Availability != volumehealth.Unavailable || h.Observation.Adverse || h.LastGood == nil || !h.LastGood.Observation.Adverse || h.LastGood.Observation.State != volumehealth.StateStale || !h.LastGood.Observation.ObservedAt.Equal(testNow) {
		t.Fatal("failure obscured current state or prior adverse evidence")
	}
	if err := ValidateView(r.Authorised(), current.ObservedAt); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-driver-reason", "private-backend-handle", `"claim"`, "pod-uid", "StorageDegraded"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("historical health leaked private text")
		}
	}
	h.LastGood.Conditions[0].Reason = "modified"
	if r.Authorised().Volumes[0].Health[0].LastGood.Conditions[0].Reason == "modified" {
		t.Fatal("historical response mutated retention")
	}
	current.Availability, current.Reason = volumehealth.Forbidden, volumehealth.AccessDenied
	if _, err := Join(s, b, u, []HealthObservation{current}, SourceState(volumehealth.Reported, ""), current.ObservedAt); err == nil {
		t.Fatal("denial retained protected history")
	}
}

func TestBackendCacheMayPredateNewPodWithoutChangingSourceClock(t *testing.T) {
	s, b, _ := fixture()
	s.CreatedAt = testNow.Add(time.Second)
	h := healthFixture(volumehealth.BackendSource, "StorageDegraded")
	r, err := Join(s, b, nil, []HealthObservation{h}, SourceState(volumehealth.Unreported, NoReport), testNow.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Authorised().Volumes[0].Health[0].Observation.ObservedAt.Equal(testNow) {
		t.Fatal("backend read time was promoted to Pod acquisition time")
	}
	h.NodeUID = "old-node"
	if _, err := Join(s, b, nil, []HealthObservation{h}, SourceState(volumehealth.Unreported, NoReport), testNow.Add(2*time.Second)); err == nil {
		t.Fatal("backend crossed Node lifetime")
	}
}

func TestHealthPayloadDeduplicatesWithoutIdentityMessageOrReadTime(t *testing.T) {
	h := healthFixture(volumehealth.PodSource, "Degraded").Observation
	a, err := NewHealthPayload(h, testNow)
	if err != nil {
		t.Fatal(err)
	}
	h.ObservedAt = testNow.Add(time.Second)
	h.Conditions[0].Message = "different-private-message"
	b, err := NewHealthPayload(h, h.ObservedAt)
	if err != nil || !a.Equal(b) {
		t.Fatal("unchanged status rewrote payload", err)
	}
	o, err := a.Observation(h.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	if o.Identity != (volumehealth.Identity{}) || o.Conditions[0].Message != "" || o.TransitionAt != h.TransitionAt || o.ObservedAt != h.ObservedAt {
		t.Fatal("payload retained identity/message or changed transition")
	}
	h.Conditions[0].Status = "Inaccessible"
	c, err := NewHealthPayload(h, h.ObservedAt)
	if err != nil || c.Equal(b) {
		t.Fatal("changed condition was suppressed", err)
	}
}
