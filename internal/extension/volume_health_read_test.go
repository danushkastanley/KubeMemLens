package extension

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestVolumeHTTPHealthProjectionPreservesUsageAndCurrentSourceState(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
		t.Fatal(err)
	}
	request := testVolumeRequest(t, now)
	if err := store.ReplaceNodeContextWithVolumes(*request.Snapshot.NodeContext, request.Snapshot.VolumeBatch); err != nil {
		t.Fatal(err)
	}
	h := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	h.now = func() time.Time { return now }
	h.volumeStatsEnabled = true
	h.volumeNamespaces = map[string]bool{"team-a": true}
	h.volumeResolver = resolverFunc(func(context.Context, string, string) (kube.ResolvedVolumes, error) {
		id := volumehealth.Identity{Namespace: "team-a", PodName: "app", PodUID: "workload-uid", NodeName: "node-a", VolumeName: "data", PVCName: "claim", PVCUID: "claim-uid", Driver: "fixture.csi.test"}
		prior := volumehealth.Observation{Identity: id, Source: volumehealth.BackendSource, Scope: volumehealth.BackendScope, Availability: volumehealth.Reported, ObservedAt: now.Add(-time.Second), Conditions: []volumehealth.Condition{{Status: "StorageDegraded", Reason: "BackendReason", Message: "private-marker"}}}
		current := volumehealth.Observation{Identity: id, Source: volumehealth.BackendSource, Scope: volumehealth.BackendScope, Availability: volumehealth.Unreported, Reason: volumehealth.NoReport, ObservedAt: now}
		return kube.ResolvedVolumes{Scope: volumecontext.PodScope{Namespace: "team-a", PodName: "app", PodUID: "workload-uid", NodeName: "node-a", NodeUID: "node-uid-a", CreatedAt: now.Add(-time.Hour)}, Bindings: []volumecontext.Binding{{VolumeName: "data", PVCName: "claim", PVCUID: "claim-uid", PVCCreatedAt: now.Add(-time.Hour), Driver: "fixture.csi.test", ClaimAvailability: volumehealth.Reported, Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim}}}, Health: []volumecontext.HealthObservation{{Observation: current, NodeUID: "node-uid-a", LastGood: &prior}}}, nil
	})
	for _, schema := range []int{4, 5} {
		r := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/app/volumes", true)
		r.Header.Set(api.SnapshotSchemaHeader, strconv.Itoa(schema))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("health read failed", w.Code)
		}
		var response api.PodVolumeContext
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		v := response.Context.Volumes[0]
		if *v.Usage.Filesystem.UsedBytes != 77 || v.Health[0].Observation.Availability != volumehealth.Unreported || v.Health[0].Observation.Adverse {
			t.Fatal("historical health changed usage or current state")
		}
		if (v.Health[0].LastGood != nil) != (schema == 5) {
			t.Fatal("historical projection did not match negotiated schema")
		}
		if strings.Contains(w.Body.String(), "private-marker") {
			t.Fatal("upstream message entered named response")
		}
		if err := volumecontext.ValidateView(response.Context, now); err != nil {
			t.Fatal(err)
		}
	}
}
