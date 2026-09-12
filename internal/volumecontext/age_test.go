package volumecontext

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestAgeViewKeepsOriginalTimesAndExpiresValues(t *testing.T) {
	s, b, u := fixture()
	r, err := Join(s, b, u, []HealthObservation{healthFixture(volumehealth.PodSource, "Degraded")}, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	view := r.Authorised()
	before, _ := json.Marshal(view)
	aged, err := AgeView(view, testNow.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if aged.Volumes[0].Usage.Freshness != volumehealth.Stale || !aged.Volumes[0].Usage.Filesystem.CapturedAt.Equal(testNow) {
		t.Fatal("age refreshed or concealed source time")
	}
	expired, err := AgeView(view, testNow.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	v := expired.Volumes[0]
	if v.Usage.Filesystem != nil || v.Usage.Reason != Expired || v.Health[0].Observation.State != volumehealth.StateStale || !v.Health[0].Observation.Adverse {
		t.Fatal("expired values or aged adverse state lost")
	}
	after, _ := json.Marshal(view)
	if string(before) != string(after) {
		t.Fatal("age mutated original evidence")
	}
	if ValidateView(expired, testNow.Add(3*time.Minute)) != nil {
		t.Fatal("aged view is invalid")
	}
}
