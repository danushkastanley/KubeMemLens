package volumecontext

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestValidateNamedViewAcceptsModelEvidenceStates(t *testing.T) {
	s, b, u := fixture()
	h := []HealthObservation{healthFixture(volumehealth.PodSource, "Degraded"), healthFixture(volumehealth.ControllerSource, ""), healthFixture(volumehealth.BackendSource, "FutureStatus")}
	for _, age := range []time.Duration{0, StaleAfter + time.Second, ExpireAfter + time.Second} {
		r, err := JoinSamples(s, b, Samples{State: SourceState(volumehealth.Reported, ""), Current: u, LastGood: u}, h, testNow.Add(age))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateView(r.Authorised(), testNow.Add(age)); err != nil {
			t.Fatal("valid model state rejected", age, err)
		}
	}
	for _, state := range []Usage{SourceState(volumehealth.Unreported, NoReport), SourceState(volumehealth.Unavailable, SourceFailed), SourceState(volumehealth.Disabled, Disabled), SourceState(volumehealth.Forbidden, AccessDenied), SourceState(volumehealth.Unsupported, Unsupported)} {
		r, err := JoinSamples(s, b, Samples{State: state, LastGood: u}, nil, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateView(r.Authorised(), testNow); err != nil {
			t.Fatal("valid capability rejected", state.Availability, err)
		}
	}
}

func TestValidateNamedViewRejectsContradictoryEvidence(t *testing.T) {
	s, b, u := fixture()
	r, err := Join(s, b, u, []HealthObservation{healthFixture(volumehealth.PodSource, "FutureStatus")}, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*View){
		func(v *View) { v.Volumes = append(v.Volumes, v.Volumes[0]) },
		func(v *View) { v.Volumes[0].Usage.Completeness = capability.Complete },
		func(v *View) { v.Volumes[0].Usage.LastGood = v.Volumes[0].Usage.Filesystem },
		func(v *View) { v.Volumes[0].Usage.Filesystem.CapturedAt = testNow.Add(FutureSkew + time.Second) },
		func(v *View) { v.Volumes[0].Health[0].Observation.Adverse = false },
		func(v *View) { v.Volumes[0].Health[0].Observation.UnknownStatus = false },
		func(v *View) { v.Volumes[0].Health[0].Observation.State = volumehealth.StateHealthy },
		func(v *View) { v.Volumes[0].Configuration.MemoryBacked = true },
	} {
		v := r.Authorised()
		mutate(&v)
		if ValidateView(v, testNow) == nil {
			t.Fatal("contradictory response accepted")
		}
	}
}
