package volumecontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

var testNow = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

func number(n uint64) *uint64 { return &n }
func fixture() (PodScope, []Binding, []RawUsage) {
	scope := PodScope{Namespace: "tenant-a", PodName: "workload", PodUID: "pod-uid", CreatedAt: testNow.Add(-time.Hour), NodeName: "node-a", NodeUID: "node-uid"}
	bindings := []Binding{{VolumeName: "data", PVCName: "claim", PVCUID: "claim-uid", PVCCreatedAt: testNow.Add(-time.Hour), Driver: "hostpath.csi.k8s.io", ClaimAvailability: volumehealth.Reported,
		Configuration: Configuration{Kind: PersistentClaim, MountCount: 1}}}
	usage := []RawUsage{{Namespace: scope.Namespace, PodUID: scope.PodUID, NodeUID: scope.NodeUID, VolumeName: "data", PVCNamespace: scope.Namespace, PVCName: "claim",
		Filesystem: Filesystem{CapturedAt: testNow, CapacityBytes: number(100), UsedBytes: number(0), AvailableBytes: number(90), Inodes: number(1000), InodesUsed: number(5)}}}
	return scope, bindings, usage
}

func healthFixture(source volumehealth.Source, status string) HealthObservation {
	scope, bindings, _ := fixture()
	b := bindings[0]
	h := volumehealth.Observation{Identity: volumehealth.Identity{Namespace: scope.Namespace, PodName: scope.PodName, PodUID: scope.PodUID, NodeName: scope.NodeName,
		VolumeName: b.VolumeName, PVCName: b.PVCName, PVCUID: b.PVCUID, Driver: b.Driver}, Source: source, Scope: volumehealth.VolumeScope,
		Availability: volumehealth.Reported, ObservedAt: testNow, TransitionAt: testNow.Add(-time.Minute)}
	if source == volumehealth.BackendSource {
		h.Scope = volumehealth.BackendScope
	}
	if status != "" {
		h.Conditions = []volumehealth.Condition{{Status: volumehealth.Status(status), Reason: "private-driver-reason", Message: "private-backend-handle"}}
	}
	return HealthObservation{Observation: h, NodeUID: scope.NodeUID}
}

func TestJoinSeparatesUsageConfigurationAndHealth(t *testing.T) {
	scope, bindings, usage := fixture()
	health := []HealthObservation{healthFixture(volumehealth.PodSource, "Degraded"), healthFixture(volumehealth.ControllerSource, ""), healthFixture(volumehealth.BackendSource, "FutureCondition")}
	r, err := Join(scope, bindings, usage, health, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	v := r.Authorised().Volumes[0]
	if v.Usage.Filesystem.UsedBytes == nil || *v.Usage.Filesystem.UsedBytes != 0 || v.Usage.Filesystem.InodesFree != nil {
		t.Fatal("missing and measured zero were conflated")
	}
	if *v.Usage.Filesystem.AvailableBytes+*v.Usage.Filesystem.UsedBytes == *v.Usage.Filesystem.CapacityBytes {
		t.Fatal("reserved-space fixture lost")
	}
	states := map[volumehealth.Source]volumehealth.Observation{}
	for _, h := range v.Health {
		states[h.Observation.Source] = h.Observation
	}
	if states[volumehealth.ControllerSource].State != volumehealth.StateHealthy || !states[volumehealth.PodSource].Adverse || !states[volumehealth.BackendSource].UnknownStatus || !states[volumehealth.BackendSource].Adverse {
		t.Fatalf("source conflict lost: %#v", states)
	}
	if states[volumehealth.ControllerSource].ProbeFreshness != volumehealth.FreshnessUnknown {
		t.Fatal("transition became probe heartbeat")
	}
	*usage[0].Filesystem.UsedBytes = 42
	*v.Usage.Filesystem.UsedBytes = 17
	if *r.Authorised().Volumes[0].Usage.Filesystem.UsedBytes != 0 {
		t.Fatal("caller mutated retained report")
	}
}

func TestJoinRejectsCrossScopeAndAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PodScope, *[]Binding, *[]RawUsage, *[]HealthObservation)
	}{
		{"namespace", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*u)[0].Namespace = "tenant-b"
		}},
		{"Pod replacement", func(s *PodScope, _ *[]Binding, _ *[]RawUsage, _ *[]HealthObservation) { s.PodUID = "new-pod" }},
		{"Node replacement", func(s *PodScope, _ *[]Binding, _ *[]RawUsage, _ *[]HealthObservation) { s.NodeUID = "new-node" }},
		{"PVC namespace", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*u)[0].PVCNamespace = "tenant-b"
		}},
		{"PVC name", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*u)[0].PVCName = "another"
		}},
		{"PVC replacement", func(_ *PodScope, b *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*b)[0].PVCCreatedAt = testNow
			(*u)[0].Filesystem.CapturedAt = testNow.Add(-time.Second)
		}},
		{"Pod lifetime", func(s *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			s.CreatedAt = testNow
			(*u)[0].Filesystem.CapturedAt = testNow.Add(-time.Second)
		}},
		{"unknown volume", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*u)[0].VolumeName = "another"
		}},
		{"duplicate binding", func(_ *PodScope, b *[]Binding, _ *[]RawUsage, _ *[]HealthObservation) {
			*b = append(*b, (*b)[0])
		}},
		{"duplicate usage", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			*u = append(*u, (*u)[0])
		}},
		{"duplicate health", func(_ *PodScope, _ *[]Binding, _ *[]RawUsage, h *[]HealthObservation) {
			*h = append(*h, (*h)[0])
		}},
		{"health tenant", func(_ *PodScope, _ *[]Binding, _ *[]RawUsage, h *[]HealthObservation) {
			(*h)[0].Identity.Namespace = "tenant-b"
		}},
		{"health UID", func(_ *PodScope, _ *[]Binding, _ *[]RawUsage, h *[]HealthObservation) {
			(*h)[0].Identity.PVCUID = "old-claim"
		}},
		{"health driver", func(_ *PodScope, _ *[]Binding, _ *[]RawUsage, h *[]HealthObservation) {
			(*h)[0].Identity.Driver = "other.csi.io"
		}},
		{"future sample", func(_ *PodScope, _ *[]Binding, u *[]RawUsage, _ *[]HealthObservation) {
			(*u)[0].Filesystem.CapturedAt = testNow.Add(time.Minute)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, b, u := fixture()
			h := []HealthObservation{healthFixture(volumehealth.ControllerSource, "Degraded")}
			tc.change(&s, &b, &u, &h)
			_, err := Join(s, b, u, h, SourceState(volumehealth.Reported, ""), testNow)
			if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrScope) {
				t.Fatalf("invalid join accepted: %v", err)
			}
			if strings.Contains(err.Error(), "tenant") || strings.Contains(err.Error(), "claim-uid") {
				t.Fatal("identity in error")
			}
		})
	}
}

func TestUsageAndHealthFreshnessRemainIndependent(t *testing.T) {
	for _, tc := range []struct {
		age       time.Duration
		freshness volumehealth.Freshness
		reason    Reason
	}{
		{StaleAfter, volumehealth.Fresh, ""}, {StaleAfter + time.Second, volumehealth.Stale, ""},
		{ExpireAfter + time.Second, volumehealth.FreshnessUnknown, Expired},
	} {
		s, b, u := fixture()
		u[0].Filesystem.CapturedAt = testNow.Add(-tc.age)
		h := healthFixture(volumehealth.PodSource, "Degraded")
		h.ObservedAt = testNow.Add(-3 * time.Minute)
		h.TransitionAt = testNow.Add(-5 * time.Minute)
		r, err := Join(s, b, u, []HealthObservation{h}, SourceState(volumehealth.Reported, ""), testNow)
		if err != nil {
			t.Fatal(err)
		}
		v := r.Authorised().Volumes[0]
		if v.Usage.Freshness != tc.freshness || v.Usage.Reason != tc.reason {
			t.Fatalf("wrong freshness: %#v", v.Usage)
		}
		if v.Health[0].Observation.State != volumehealth.StateStale || !v.Health[0].Observation.Adverse {
			t.Fatal("staleness erased adverse health")
		}
		if tc.reason == Expired && v.Usage.Filesystem != nil {
			t.Fatal("expired usage retained")
		}
	}
}

func TestSourceStatesAndMissingUsage(t *testing.T) {
	for availability, reason := range map[volumehealth.Availability]Reason{
		volumehealth.Reported: "", volumehealth.Unreported: NoReport, volumehealth.Disabled: Disabled,
		volumehealth.Unsupported: Unsupported, volumehealth.Forbidden: AccessDenied, volumehealth.Unavailable: SourceFailed, volumehealth.Unknown: BindingUnavailable,
	} {
		s, b, _ := fixture()
		r, err := Join(s, b, nil, nil, SourceState(availability, reason), testNow)
		if err != nil {
			t.Fatal(err)
		}
		u := r.Authorised().Volumes[0].Usage
		if availability == volumehealth.Reported {
			availability, reason = volumehealth.Unreported, NoReport
		}
		if u.Availability != availability || u.Reason != reason || u.Filesystem != nil {
			t.Fatal("missing usage was fabricated")
		}
	}
}

func TestInlineEphemeralAndMemoryBackedConfiguration(t *testing.T) {
	s, b, _ := fixture()
	b[0].Configuration.Kind = EphemeralClaim
	b = append(b, Binding{VolumeName: "inline", Driver: "hostpath.csi.k8s.io", Configuration: Configuration{Kind: InlineCSI}},
		Binding{VolumeName: "tmpfs", Configuration: Configuration{Kind: EmptyDir, MemoryBacked: true, SizeLimitBytes: number(1024), MountCount: 2, ReadOnlyMountCount: 1}})
	r, err := Join(s, b, nil, nil, SourceState(volumehealth.Disabled, Disabled), testNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Authorised().Volumes) != 3 || !r.Authorised().Volumes[2].Configuration.MemoryBacked {
		t.Fatal("configuration lost")
	}
	*b[2].Configuration.SizeLimitBytes = 42
	if *r.Authorised().Volumes[2].Configuration.SizeLimitBytes != 1024 {
		t.Fatal("configuration shares input")
	}
	bad := b[1]
	bad.PVCName = "unexpected"
	if _, err := Join(s, []Binding{bad}, nil, nil, SourceState(volumehealth.Disabled, Disabled), testNow); err == nil {
		t.Fatal("inline PVC accepted")
	}
}

func TestRedactionAndAuthorisedRoundTrip(t *testing.T) {
	s, b, u := fixture()
	r, err := Join(s, b, u, []HealthObservation{healthFixture(volumehealth.ControllerSource, "Degraded")}, SourceState(volumehealth.Reported, ""), testNow)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r.Authorised())
	if err != nil {
		t.Fatal(err)
	}
	var view View
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view, r.Authorised()) {
		t.Fatal("named contract did not round trip")
	}
	if !strings.Contains(string(data), "private-driver-reason") || !strings.Contains(string(data), "claim") || strings.Contains(string(data), "private-backend-handle") {
		t.Fatal("wrong named exposure")
	}
	redacted, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/redacted.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(redacted) != strings.TrimSpace(string(golden)) {
		t.Fatalf("redacted wire differs from golden: %s", redacted)
	}
	for _, secret := range []string{"tenant-a", "workload", "claim-uid", "pod-uid", "node-uid", "hostpath.csi", "private-driver-reason", "private-backend-handle", `"volumeName"`, `"pvcName"`} {
		if strings.Contains(string(redacted), secret) || strings.Contains(fmt.Sprintf("%+v %#v", r, r), secret) {
			t.Fatalf("redacted output leaked %q", secret)
		}
	}
	var roundtrip Redacted
	if err := json.Unmarshal(redacted, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundtrip, r.Redacted()) {
		t.Fatal("redacted contract did not round trip")
	}
	if roundtrip.Volumes[0].Health[0].State != volumehealth.StateAdverse {
		t.Fatal("redaction erased health")
	}
	if got := r.Summary(); got.Volumes != 1 || got.UsageReported != 1 || got.AdverseReports != 1 {
		t.Fatalf("wrong metrics summary: %+v", got)
	}
}
