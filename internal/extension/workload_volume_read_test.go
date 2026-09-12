package extension

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

type workloadResolverFunc func(context.Context, string, string, string) (kube.ResolvedWorkloadVolumes, error)

func (workloadResolverFunc) Resolve(context.Context, string, string) (kube.ResolvedVolumes, error) {
	return kube.ResolvedVolumes{}, volumecontext.ErrInvalid
}
func (f workloadResolverFunc) ResolveWorkload(ctx context.Context, ns, kind, name string) (kube.ResolvedWorkloadVolumes, error) {
	return f(ctx, ns, kind, name)
}

func TestWorkloadVolumeHTTPUsesLiveScopeAndDeduplicatesClaims(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	resolved := kube.ResolvedWorkloadVolumes{Namespace: "team-a", Kind: "Deployment", Name: "app", UID: "workload-uid", ObservedAt: now}
	var containers []api.ContainerSnapshot
	for _, name := range []string{"pod-a", "pod-b"} {
		resolved.Pods = append(resolved.Pods, kube.ResolvedVolumes{Scope: volumecontext.PodScope{Namespace: "team-a", PodName: name, PodUID: name + "-uid", NodeName: "node-a", NodeUID: "node-uid", CreatedAt: now.Add(-time.Hour)}, Bindings: []volumecontext.Binding{{VolumeName: "data", PVCName: "shared-claim", PVCUID: "claim-uid", PVUID: "pv-uid", PVCCreatedAt: now.Add(-time.Hour), ClaimAvailability: volumehealth.Reported, Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim, MountCount: 1}}}})
		containers = append(containers, api.ContainerSnapshot{Namespace: "team-a", PodName: name, PodUID: name + "-uid", ContainerName: "app", ContainerID: name + "-runtime", Memory: model.MemoryBreakdown{TotalBytes: 100}})
	}
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now, Containers: containers}); err != nil {
		t.Fatal(err)
	}
	h := NewReadHandler(store, collector.DefaultHandlerOptions(time.Minute))
	h.volumeWorkloadsEnabled = true
	h.volumeNamespaces = map[string]bool{"team-a": true}
	h.now = func() time.Time { return now }
	calls := 0
	h.volumeResolver = workloadResolverFunc(func(ctx context.Context, ns, kind, name string) (kube.ResolvedWorkloadVolumes, error) {
		calls++
		principal, _ := apirequest.UserFrom(ctx)
		if principal.GetName() != "tenant-reader" || ns != "team-a" || kind != "Deployment" || name != "app" {
			t.Fatal("caller or target lost")
		}
		return resolved, nil
	})
	deny := false
	authorised := 0
	h.podAuthorizer = authorizer.AuthorizerFunc(func(_ context.Context, a authorizer.Attributes) (authorizer.Decision, string, error) {
		if a.GetAPIGroup() != api.MemoryAPIGroup || a.GetAPIVersion() != api.MemoryAPIVersion || a.GetResource() != "pods" || a.GetVerb() != "get" || a.GetNamespace() != "team-a" || a.GetUser().GetName() != "tenant-reader" {
			t.Fatal("incorrect memory disclosure check")
		}
		authorised++
		if deny {
			return authorizer.DecisionDeny, "", nil
		}
		return authorizer.DecisionAllow, "", nil
	})
	read := func(schema string) *httptest.ResponseRecorder {
		r := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/workloads/app/volumes?kind=deployment", true)
		r.Header.Set(api.SnapshotSchemaHeader, schema)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := read("6")
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var result api.WorkloadVolumeContext
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || api.ValidateWorkloadVolumeContext(result, now) != nil {
		t.Fatal("response failed bounded production decoder")
	}
	if result.Workload.Memory.TotalBytes != 200 || result.Workload.PodCount != 2 || len(result.Filesystems) != 1 || len(result.Filesystems[0].Members) != 2 || authorised != 2 {
		t.Fatal("memory changed or shared filesystem duplicated")
	}
	before := calls
	if read("5").Code != 404 || calls != before {
		t.Fatal("old schema reached workload query")
	}
	deny = true
	w = read("6")
	if w.Code != 403 || strings.Contains(w.Body.String(), "shared-claim") || strings.Contains(w.Body.String(), "pod-a") {
		t.Fatal("revoked response exposed cached identities")
	}
	deny = false
	containers = containers[:1]
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: containers}); err != nil {
		t.Fatal(err)
	}
	w = read("6")
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("decode")
	}
	if result.Workload.PodCount != 1 || result.Workload.Completeness != api.EvidencePartial || len(result.PodVolumes) != 2 {
		t.Fatal("missing memory became measured zero or removed live volume context")
	}
}
