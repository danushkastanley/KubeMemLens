package volumecontext

import (
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestRetainDoesNotRefreshOmittedSamplesOrCrossNodeLifetimes(t *testing.T) {
	previous := batchFixture(t)
	s, _, _ := fixture()
	at := testNow.Add(15 * time.Second)
	missing, err := NewBatch(s.NodeName, s.NodeUID, at, SourceState(volumehealth.Reported, ""), nil, at)
	if err != nil {
		t.Fatal(err)
	}
	good, err := Retain(&previous, missing, at)
	if err != nil {
		t.Fatal(err)
	}
	if good.Len() != 1 || !good.ForPod(s)[0].Filesystem.CapturedAt.Equal(testNow) {
		t.Fatal("omission refreshed or erased the last good sample")
	}
	expiredAt := testNow.Add(ExpireAfter + time.Second)
	expired, err := NewBatch(s.NodeName, s.NodeUID, expiredAt, SourceState(volumehealth.Reported, ""), nil, expiredAt)
	if err != nil {
		t.Fatal(err)
	}
	good, err = Retain(&previous, expired, expiredAt)
	if err != nil || good.Len() != 0 {
		t.Fatal("expired sample retained", err)
	}
	replacement, err := NewBatch(s.NodeName, "replacement-node", at, SourceState(volumehealth.Reported, ""), nil, at)
	if err != nil {
		t.Fatal(err)
	}
	good, err = Retain(&previous, replacement, at)
	if err != nil || good.Len() != 0 {
		t.Fatal("Node replacement retained old data", err)
	}
}

func TestRetainRejectsCounterfeitOlderAndChangedSameTimeSamples(t *testing.T) {
	previous := batchFixture(t)
	s, _, u := fixture()
	for _, change := range []func(*RawUsage){
		func(r *RawUsage) { r.Filesystem.CapturedAt = testNow.Add(-time.Second) },
		func(r *RawUsage) { r.Filesystem.UsedBytes = number(42) },
		func(r *RawUsage) { r.Filesystem.InodesFree = number(0) },
	} {
		row := u[0]
		change(&row)
		current, err := NewBatch(s.NodeName, s.NodeUID, testNow.Add(time.Second), SourceState(volumehealth.Reported, ""), []RawUsage{row}, testNow.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Retain(&previous, current, testNow.Add(time.Second)); !errors.Is(err, ErrOutOfOrder) {
			t.Fatal("altered source sample accepted", err)
		}
	}
}

func TestJoinSamplesRetainsOnlyAuthorisedLastGoodEvidence(t *testing.T) {
	s, b, u := fixture()
	at := testNow.Add(15 * time.Second)
	for _, state := range []Usage{SourceState(volumehealth.Unavailable, SourceFailed), SourceState(volumehealth.Reported, "")} {
		r, err := JoinSamples(s, b, Samples{State: state, LastGood: u}, nil, at)
		if err != nil {
			t.Fatal(err)
		}
		usage := r.Authorised().Volumes[0].Usage
		if usage.Filesystem != nil || usage.LastGood == nil || !usage.LastGood.CapturedAt.Equal(testNow) || usage.Freshness != volumehealth.Stale {
			t.Fatal("last good evidence was promoted or lost")
		}
		*usage.LastGood.UsedBytes = 99
		if *r.Authorised().Volumes[0].Usage.LastGood.UsedBytes != 0 {
			t.Fatal("returned cache sample changed retained data")
		}
	}
	for _, state := range []Usage{SourceState(volumehealth.Disabled, Disabled), SourceState(volumehealth.Forbidden, AccessDenied), SourceState(volumehealth.Unsupported, Unsupported)} {
		r, err := JoinSamples(s, b, Samples{State: state, LastGood: u}, nil, at)
		if err != nil {
			t.Fatal(err)
		}
		if r.Authorised().Volumes[0].Usage.LastGood != nil {
			t.Fatal("confirmed unavailable source retained protected output")
		}
	}
	b[0] = Binding{VolumeName: "data", ClaimAvailability: volumehealth.Forbidden, Configuration: Configuration{Kind: PersistentClaim}}
	r, err := JoinSamples(s, b, Samples{State: SourceState(volumehealth.Reported, ""), Current: u, LastGood: u}, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	row := r.Authorised().Volumes[0]
	if row.PVCName != "" || row.Usage.Filesystem != nil || row.Usage.LastGood != nil || row.Usage.Availability != volumehealth.Forbidden {
		t.Fatal("denied binding exposed cached data")
	}
}

func TestJoinSamplesExpiresRecreatedClaimAndChecksInputBounds(t *testing.T) {
	s, b, u := fixture()
	b[0].PVCCreatedAt = testNow.Add(time.Second)
	r, err := JoinSamples(s, b, Samples{State: SourceState(volumehealth.Reported, ""), Current: u, LastGood: u}, nil, testNow.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if row := r.Authorised().Volumes[0]; row.Usage.Filesystem != nil || row.Usage.LastGood != nil {
		t.Fatal("recreated claim inherited old measurements")
	}
	if _, err := JoinSamples(s, b, Samples{State: SourceState(volumehealth.Reported, ""), Current: make([]RawUsage, MaxVolumesPerPod+1)}, nil, testNow); err == nil {
		t.Fatal("input count was bounded only after filtering")
	}
}
