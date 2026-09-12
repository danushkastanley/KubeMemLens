package volumecontext

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestUnavailableClaimRetainsOnlyPodConfiguration(t *testing.T) {
	s, b, u := fixture()
	for _, availability := range []volumehealth.Availability{volumehealth.Forbidden, volumehealth.Unavailable, volumehealth.Unreported} {
		binding := Binding{VolumeName: b[0].VolumeName, ClaimAvailability: availability, Configuration: b[0].Configuration}
		r, err := Join(s, []Binding{binding}, nil, nil, SourceState(volumehealth.Reported, ""), testNow)
		if err != nil {
			t.Fatal(err)
		}
		row := r.Authorised().Volumes[0]
		if row.PVCName != "" || row.Driver != "" || row.Usage.Availability != availability || row.Usage.Filesystem != nil || row.Configuration.Kind != PersistentClaim {
			t.Fatal("wrong denied view")
		}
		if _, err := Join(s, []Binding{binding}, u, nil, SourceState(volumehealth.Reported, ""), testNow); err == nil {
			t.Fatal("raw usage admitted without claim access")
		}
		binding.PVCName = "secret-claim"
		if _, err := Join(s, []Binding{binding}, nil, nil, SourceState(volumehealth.Reported, ""), testNow); err == nil {
			t.Fatal("denied claim identity retained")
		}
	}
}

func TestHealthScopeAndCapabilityValidation(t *testing.T) {
	s, b, _ := fixture()
	for name, mutate := range map[string]func(*HealthObservation){
		"Node lifetime":               func(h *HealthObservation) { h.NodeUID = "old-node" },
		"controller without claim":    func(h *HealthObservation) { h.Identity.PVCUID = "" },
		"future transition":           func(h *HealthObservation) { h.TransitionAt = testNow.Add(time.Minute) },
		"future condition transition": func(h *HealthObservation) { h.Conditions[0].TransitionAt = testNow.Add(time.Minute) },
		"unknown source":              func(h *HealthObservation) { h.Source = "future-source" },
		"wrong source scope":          func(h *HealthObservation) { h.Scope = volumehealth.BackendScope },
		"unknown availability":        func(h *HealthObservation) { h.Availability = "future-availability" },
		"unsupported with conditions": func(h *HealthObservation) {
			h.Availability = volumehealth.Unsupported
			h.Reason = volumehealth.NoReport
		},
		"reported with failure reason": func(h *HealthObservation) { h.Reason = volumehealth.AccessDenied },
		"unbounded identity":           func(h *HealthObservation) { h.Identity.Driver = strings.Repeat("a", MaxNameBytes+1) },
	} {
		t.Run(name, func(t *testing.T) {
			h := healthFixture(volumehealth.ControllerSource, "Degraded")
			mutate(&h)
			if _, err := Join(s, b, nil, []HealthObservation{h}, SourceState(volumehealth.Unreported, NoReport), testNow); err == nil {
				t.Fatal("invalid health accepted")
			}
		})
	}
	for _, availability := range []volumehealth.Availability{volumehealth.Disabled, volumehealth.Unsupported, volumehealth.Unreported, volumehealth.Forbidden, volumehealth.Unavailable, volumehealth.Unknown} {
		h := healthFixture(volumehealth.ControllerSource, "")
		h.Availability = availability
		h.Reason = volumehealth.NoReport
		r, err := Join(s, b, nil, []HealthObservation{h}, SourceState(volumehealth.Unreported, NoReport), testNow)
		if err != nil {
			t.Fatal(err)
		}
		state := r.Authorised().Volumes[0].Health[0].Observation
		if state.State == volumehealth.StateHealthy || state.Availability != availability {
			t.Fatal("missing capability became healthy")
		}
	}
}

func TestUsageCompletenessDoesNotDependOnNonzeroValues(t *testing.T) {
	s, b, u := fixture()
	u[0].Filesystem.InodesFree = number(0)
	r, err := Join(s, b, u, nil, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authorised().Volumes[0].Usage.Completeness != capability.Complete {
		t.Fatal("measured zero made coverage partial")
	}
	u[0].Filesystem.CapacityBytes = nil
	r, err = Join(s, b, u, nil, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if r.Authorised().Volumes[0].Usage.Completeness != capability.Partial {
		t.Fatal("missing capacity made coverage complete")
	}
}

func TestMaximumBatchAndNamedResponseByteBound(t *testing.T) {
	s, b, u := fixture()
	records := make([]RawUsage, MaxBatchRecords)
	for i := range records {
		records[i] = u[0]
		records[i].PodUID = fmt.Sprintf("pod-%d", i)
	}
	batch, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState(volumehealth.Reported, ""), records, testNow)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := batch.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxBatchBytes {
		t.Fatal("batch exceeds budget")
	}
	if _, err := DecodePrivate(encoded, testNow); err != nil {
		t.Fatal(err)
	}
	bindings := make([]Binding, MaxVolumesPerPod)
	health := make([]HealthObservation, MaxVolumesPerPod)
	for i := range bindings {
		bindings[i] = b[0]
		bindings[i].VolumeName = fmt.Sprintf("volume-%d", i)
		health[i] = healthFixture(volumehealth.PodSource, "Degraded")
		health[i].Identity.VolumeName = bindings[i].VolumeName
		condition := health[i].Conditions[0]
		condition.Reason = strings.Repeat("x", MaxReasonBytes)
		health[i].Conditions = make([]volumehealth.Condition, volumehealth.MaxConditions)
		for j := range health[i].Conditions {
			health[i].Conditions[j] = condition
		}
	}
	// Individual fields meet their limits, but their combined named response is
	// too large. The byte ceiling must reject it, independently of list bounds.
	if _, err := Join(s, bindings, nil, health, SourceState(volumehealth.Unreported, NoReport), testNow); err == nil {
		t.Fatal("combined response exceeded byte limit")
	}
	if _, err := Join(s, bindings, nil, nil, SourceState(volumehealth.Unreported, NoReport), testNow); err != nil {
		t.Fatal("bounded maximum list rejected")
	}
}

func TestInvalidScopeAndSourceState(t *testing.T) {
	s, b, _ := fixture()
	for _, mutate := range []func(*PodScope){func(s *PodScope) { s.Namespace = "" }, func(s *PodScope) { s.PodUID = "" }, func(s *PodScope) { s.CreatedAt = time.Time{} }, func(s *PodScope) { s.NodeUID = "bad\x00uid" }} {
		copy := s
		mutate(&copy)
		if _, err := Join(copy, b, nil, nil, SourceState(volumehealth.Unreported, NoReport), testNow); err == nil {
			t.Fatal("invalid scope accepted")
		}
	}
	if _, err := Join(s, b, nil, nil, SourceState(volumehealth.Unknown, "invented"), testNow); err == nil {
		t.Fatal("unknown source reason accepted")
	}
	if _, err := NewBatch(s.NodeName, s.NodeUID, testNow, SourceState(volumehealth.Unreported, NoReport), []RawUsage{{}}, testNow); err == nil {
		t.Fatal("failure batch had records")
	}
	data, err := json.Marshal(Binding{PVCName: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatal("default binding encoder exposed identity")
	}
}
