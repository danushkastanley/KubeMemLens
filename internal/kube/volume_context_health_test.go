package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
)

func newHealthResolverFixture(t *testing.T) *volumeResolverFixture {
	t.Helper()
	f := newVolumeResolverFixture(t)
	base := f.resolver.(*volumeResolver)
	r, err := NewHealthVolumeResolver(t.Context(), f.config, base.authorize, base.nodeUID)
	if err != nil {
		t.Fatal(err)
	}
	f.resolver = r
	f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "data", HealthConditions: []corev1.VolumeHealthCondition{{Status: "FutureDiskState", Reason: "PodReason", Message: "private-pod-message"}}}}
	f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{}
	f.csinode.Status.StorageHealth = []storagev1.StorageHealth{{Name: "unrelated-driver", HealthConditions: []storagev1.StorageHealthCondition{{Status: "StorageUnreachable", Message: "other-tenant"}}}, {Name: "fixture.csi.test", HealthConditions: []storagev1.StorageHealthCondition{{Status: "StorageDegraded", Reason: "BackendReason", Message: "private-backend-message"}}}}
	return f
}

func readResolvedHealth(t *testing.T, f *volumeResolverFixture) (volumecontext.Report, map[volumehealth.Source]volumecontext.Health) {
	t.Helper()
	v, err := f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil {
		t.Fatal(err)
	}
	r, err := volumecontext.Join(v.Scope, v.Bindings, nil, v.Health, volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := volumecontext.ValidateView(r.Authorised(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	bySource := map[volumehealth.Source]volumecontext.Health{}
	for _, h := range r.Authorised().Volumes[0].Health {
		bySource[h.Observation.Source] = h
	}
	return r, bySource
}

func TestHealthResolverPreservesConflictsAndOriginalCallerPermissions(t *testing.T) {
	f := newHealthResolverFixture(t)
	r, rows := readResolvedHealth(t, f)
	if len(rows) != 3 || !rows[volumehealth.PodSource].Observation.UnknownStatus || !rows[volumehealth.PodSource].Observation.Adverse || rows[volumehealth.ControllerSource].Observation.State != volumehealth.StateHealthy || !rows[volumehealth.BackendSource].Observation.Adverse {
		t.Fatal("source-separated conflicting reports lost")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-", "PodReason", "BackendReason", "other-tenant", "unrelated-driver", "FutureDiskState"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("default export leaked source text")
		}
	}
	c := f.resolver.(*volumeResolver).health
	before := c.stats()
	_, _ = readResolvedHealth(t, f)
	after := c.stats()
	if after.Reads != 1 || after.PayloadWrites != before.PayloadWrites {
		t.Fatal("unchanged status caused repeated backend read or retained writes")
	}
	f.denied["csinodes"] = true
	_, rows = readResolvedHealth(t, f)
	if rows[volumehealth.BackendSource].Observation.Availability != volumehealth.Forbidden || rows[volumehealth.BackendSource].LastGood != nil || rows[volumehealth.ControllerSource].Observation.State != volumehealth.StateHealthy {
		t.Fatal("warm backend denial leaked cache or hid controller")
	}
	f.denied["csinodes"] = false
	f.denied["persistentvolumes"] = true
	r, rows = readResolvedHealth(t, f)
	if r.Authorised().Volumes[0].Driver != "" || rows[volumehealth.BackendSource].Observation.Availability != volumehealth.Forbidden || rows[volumehealth.BackendSource].LastGood != nil {
		t.Fatal("PV denial leaked backend cache or driver")
	}
	f.denied["persistentvolumes"] = false
	f.denied["persistentvolumeclaims"] = true
	r, rows = readResolvedHealth(t, f)
	if r.Authorised().Volumes[0].PVCName != "" || rows[volumehealth.ControllerSource].Observation.Availability != volumehealth.Forbidden || rows[volumehealth.BackendSource].Observation.Availability != volumehealth.Forbidden || !rows[volumehealth.PodSource].Observation.Adverse {
		t.Fatal("source-specific PVC denial changed Pod report or retained protected sources")
	}
}

func TestHealthResolverMissingUnsupportedAndDisabledRemainDistinct(t *testing.T) {
	f := newHealthResolverFixture(t)
	f.pod.Status.VolumeHealth = nil
	f.pvc.Status.HealthStatus = nil
	f.csinode.Status.StorageHealth = nil
	_, rows := readResolvedHealth(t, f)
	for _, h := range rows {
		if h.Observation.Availability != volumehealth.Unreported || h.Observation.State == volumehealth.StateHealthy {
			t.Fatal("missing alpha fields became healthy")
		}
	}
	f.pv.Spec.CSI = nil
	_, rows = readResolvedHealth(t, f)
	if rows[volumehealth.BackendSource].Observation.Availability != volumehealth.Unsupported || rows[volumehealth.BackendSource].Observation.Reason != volumehealth.NotCSIVolume {
		t.Fatal("known non-CSI binding lost its capability")
	}
	off := newVolumeResolverFixture(t)
	_, rows = readResolvedHealth(t, off)
	for _, h := range rows {
		if h.Observation.Availability != volumehealth.Disabled {
			t.Fatal("disabled acquisition became absent data")
		}
	}
}

func TestBackendOutageKeepsStaleAdverseAndChecksAccessBeforeRetention(t *testing.T) {
	f := newHealthResolverFixture(t)
	_, rows := readResolvedHealth(t, f)
	at := rows[volumehealth.BackendSource].Observation.ObservedAt
	f.backendStatus = http.StatusServiceUnavailable
	c := f.resolver.(*volumeResolver).health
	// Advance eligibility without sleeping; source time must remain unchanged.
	c.mu.Lock()
	for key, e := range c.entries {
		if key.source == volumehealth.BackendSource {
			e.attemptedAt = time.Time{}
			c.entries[key] = e
		}
	}
	c.mu.Unlock()
	_, rows = readResolvedHealth(t, f)
	h := rows[volumehealth.BackendSource]
	if h.Observation.Availability != volumehealth.Unavailable || h.LastGood == nil || !h.LastGood.Observation.Adverse || h.LastGood.Observation.State != volumehealth.StateStale || h.LastGood.Observation.ObservedAt != at {
		t.Fatal("backend outage erased or promoted old adverse report")
	}
	f.denied["csinodes"] = true
	_, rows = readResolvedHealth(t, f)
	if rows[volumehealth.BackendSource].LastGood != nil || rows[volumehealth.BackendSource].Observation.Availability != volumehealth.Forbidden {
		t.Fatal("retention bypassed revoked source access")
	}
}

func TestHealthResolverRechecksNodeLifetimeAfterReads(t *testing.T) {
	f := newHealthResolverFixture(t)
	r := f.resolver.(*volumeResolver)
	calls := 0
	r.nodeUID = func(string, time.Time) (string, bool) {
		calls++
		if calls == 1 {
			return "node-uid", true
		}
		return "replacement-node", true
	}
	if _, err := r.Resolve(context.Background(), "tenant-a", "app"); err == nil {
		t.Fatal("Node replacement during health read accepted")
	}
}
