package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
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
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func testVolumeRequest(t *testing.T, now time.Time) api.NodeSnapshotRequest {
	t.Helper()
	r := nodeRequest(now, 1)
	r.Snapshot.SchemaVersion = api.VolumeSnapshotSchemaVersion
	used := uint64(77)
	b, err := volumecontext.NewBatch("node-a", "node-uid-a", now, volumecontext.SourceState(volumehealth.Reported, ""), []volumecontext.RawUsage{{Namespace: "team-a", PodUID: "workload-uid", NodeUID: "node-uid-a", VolumeName: "data", PVCNamespace: "team-a", PVCName: "claim", Filesystem: volumecontext.Filesystem{CapturedAt: now, UsedBytes: &used}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	r.Snapshot.VolumeBatch, err = b.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestVolumeHTTPIngestionIsExplicitAndRoleBound(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		now := time.Now().UTC()
		store := collector.NewStore()
		if err := store.ReconcileNodeIdentities(map[string]string{"node-a": "node-uid-a"}, now); err != nil {
			t.Fatal(err)
		}
		coordinator := testCoordinator(t, store, now, 10)
		h, err := NewHandler(coordinator, HandlerOptions{AgentUsername: "cgroup-reader", NodeContextUsername: "node-reader", VolumeStatsEnabled: enabled, MaxSnapshotBytes: 2 << 20, MaxConcurrent: 2, RequestsPerSec: 100, Burst: 100, MaxIdentities: 10})
		if err != nil {
			t.Fatal(err)
		}
		mux := newTestRouteMux()
		h.Register(mux)
		request := testVolumeRequest(t, now)
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		post := func(account string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(http.MethodPost, "/apis/memory.kubememlens.io/v1alpha1/nodesnapshots", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r = r.WithContext(apirequest.WithUser(r.Context(), &user.DefaultInfo{Name: account, Extra: map[string][]string{PodUIDExtra: {"producer-pod"}, NodeNameExtra: {"node-a"}, NodeUIDExtra: {"node-uid-a"}, CredentialIDExtra: {"JTI=fixture"}}}))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			return w
		}
		if w := post("cgroup-reader"); w.Code != http.StatusForbidden {
			t.Fatalf("cgroup role accepted volume batch: %d", w.Code)
		}
		w := post("node-reader")
		want := http.StatusForbidden
		if enabled {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Fatalf("enabled=%v status=%d body=%s", enabled, w.Code, w.Body.String())
		}
		if enabled {
			if w := post("node-reader"); w.Code != 200 || !strings.Contains(w.Body.String(), `"duplicate":true`) {
				t.Fatal("volume replay idempotency lost")
			}
		}
	}
}

type resolverFunc func(context.Context, string, string) (kube.ResolvedVolumes, error)

func (f resolverFunc) Resolve(ctx context.Context, ns, pod string) (kube.ResolvedVolumes, error) {
	return f(ctx, ns, pod)
}

func TestVolumeReadPreservesCallerScopeAndRevocation(t *testing.T) {
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
	deny := false
	calls := 0
	h.volumeResolver = resolverFunc(func(ctx context.Context, ns, pod string) (kube.ResolvedVolumes, error) {
		calls++
		principal, _ := apirequest.UserFrom(ctx)
		if principal.GetName() != "tenant-reader" {
			t.Fatal("original caller missing")
		}
		if deny {
			return kube.ResolvedVolumes{}, &kube.HealthReadError{Reason: volumehealth.AccessDenied}
		}
		return kube.ResolvedVolumes{Scope: volumecontext.PodScope{Namespace: ns, PodName: pod, PodUID: "workload-uid", NodeName: "node-a", NodeUID: "node-uid-a", CreatedAt: now.Add(-time.Hour)}, Bindings: []volumecontext.Binding{{VolumeName: "data", PVCName: "claim", PVCUID: "claim-uid", PVCCreatedAt: now.Add(-time.Hour), ClaimAvailability: volumehealth.Reported, Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim}}}}, nil
	})
	read := func(ns, schema string) *httptest.ResponseRecorder {
		r := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/"+ns+"/pods/app/volumes", true)
		r.Header.Set(api.SnapshotSchemaHeader, schema)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := read("team-a", strconv.Itoa(api.VolumeSnapshotSchemaVersion)); w.Code != 200 || !strings.Contains(w.Body.String(), `"usedBytes":77`) {
		t.Fatalf("authorised usage missing: %d %s", w.Code, w.Body.String())
	}
	before := calls
	for _, pair := range [][2]string{{"team-b", "4"}, {"team-a", "3"}} {
		if w := read(pair[0], pair[1]); w.Code != 404 {
			t.Fatal("unsupported scope/representation accepted")
		}
	}
	if calls != before {
		t.Fatal("unsupported scope reached resolver")
	}
	deny = true
	if w := read("team-a", "4"); w.Code != 403 || strings.Contains(w.Body.String(), "claim") {
		t.Fatal("revoked read exposed retained data")
	}
	// Generic API authorisation precedes ReadHandler; the additional resolver
	// callback must preserve the original subject, groups and extras as well.
	h.podAuthorizer = authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		if a.GetUser().GetName() != "reader" || a.GetUser().GetGroups()[0] != "team" || a.GetUser().GetExtra()["bound"][0] != "identity" || a.GetNamespace() != "team-a" || a.GetName() != "claim" || a.GetResource() != "persistentvolumeclaims" {
			t.Fatal("delegated object attributes changed")
		}
		return authorizer.DecisionAllow, "", nil
	})
	ctx := apirequest.WithUser(t.Context(), &user.DefaultInfo{Name: "reader", Groups: []string{"team"}, Extra: map[string][]string{"bound": {"identity"}}})
	if err := h.authoriseVolumeObject(ctx, kube.VolumeAccess{Resource: "persistentvolumeclaims", Namespace: "team-a", Name: "claim"}); err != nil {
		t.Fatal(err)
	}
}

func TestVolumeWireRetainsPreallocationBounds(t *testing.T) {
	now := time.Now().UTC()
	body, err := json.Marshal(testVolumeRequest(t, now))
	if err != nil {
		t.Fatal(err)
	}
	if boundedNodeWire(body) == nil {
		t.Fatal("legacy Node-only wire admitted records")
	}
	if err := boundedNodeWireWithVolumes(body, volumecontext.MaxBatchRecords); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{
		[]byte(`{"snapshot":{"containers":[{}]}}`),
		[]byte(`{"snapshot":{"volumeBatch":{"records":[` + strings.Repeat(`{},`, volumecontext.MaxBatchRecords) + `{}]}}}`),
	} {
		if boundedNodeWireWithVolumes(body, volumecontext.MaxBatchRecords) == nil {
			t.Fatal("volume mode loosened another collection boundary")
		}
	}
}
