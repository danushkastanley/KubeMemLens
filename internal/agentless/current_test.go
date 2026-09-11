package agentless

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"k8s.io/client-go/rest"
)

func TestCurrentJoinsCallerScopedMetadataMetricsAndGroups(t *testing.T) {
	reader, fixture := newCurrentFixture(t, Options{})
	batch, err := reader.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Pods) != 1 || len(batch.Nodes) != 1 || len(batch.Workloads) != 1 || len(batch.Namespaces) != 1 {
		t.Fatalf("unexpected row counts: %+v", batch)
	}
	pod, node := batch.Pods[0], batch.Nodes[0]
	if pod.Cgroup != nil || pod.Containers[0].Cgroup != nil || pod.WorkingSet.Bytes == nil || *pod.WorkingSet.Bytes != 32<<20 {
		t.Fatalf("incorrect memory semantics: %+v", pod)
	}
	if pod.Context.CreatedAt.Unix() != fixture.pod.CreationTimestamp.Unix() || pod.Context.WorkloadKind != "Deployment" || pod.Context.WorkloadName != "app" {
		t.Fatalf("metadata: %+v", pod.Context)
	}
	if pod.Containers[0].WorkingSet.Evidence.Window != 15*time.Second || !pod.Containers[0].WorkingSet.Evidence.CapturedAt.Equal(fixture.at) || pod.Containers[0].WorkingSet.Evidence.Stability != capability.Beta {
		t.Fatal("lost provider evidence")
	}
	if pod.Containers[1].WorkingSet.Bytes != nil || pod.WorkingSet.Coverage.Reported != 1 || pod.WorkingSet.Coverage.Expected != 2 || batch.Completeness != capability.Partial {
		t.Fatal("missing container disguised as zero or complete")
	}
	if node.WorkingSet.Bytes == nil || *node.WorkingSet.Bytes != 512<<20 || node.CapacityMemoryBytes == nil || *node.CapacityMemoryBytes != 2<<30 || node.MemoryPressure != "healthy" {
		t.Fatalf("node: %+v", node)
	}
	if *batch.Workloads[0].WorkingSet.Bytes != 32<<20 || *batch.Namespaces[0].WorkingSet.Bytes != 32<<20 {
		t.Fatal("group sum included full Node memory")
	}
}

func TestCurrentRefreshReauthorisesRequiredAndOptionalReads(t *testing.T) {
	reader, fixture := newCurrentFixture(t, Options{})
	if _, err := reader.Current(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture.denyOwners.Store(true)
	fixture.denyNodes.Store(true)
	fixture.denyMetrics.Store(true)
	batch, err := reader.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Pods) != 1 || batch.Pods[0].OwnerAvailability != capability.Forbidden || batch.Pods[0].Context.WorkloadKind == "Deployment" || fixture.ownerReads.Load() != 2 {
		t.Fatal("owner permission cache crossed refreshes")
	}
	if batch.Pods[0].WorkingSet.Bytes != nil || batch.Pods[0].WorkingSet.Availability != capability.Forbidden || batch.Pods[0].Containers[0].Context.MemoryRequestBytes != 64<<20 {
		t.Fatalf("denied metrics lost rows or became zero: %+v", batch.Pods[0])
	}
	if batch.Nodes[0].StatusAvailability != capability.Forbidden || batch.Nodes[0].CapacityMemoryBytes != nil || batch.Nodes[0].WorkingSet.Bytes != nil {
		t.Fatal("retained revoked node evidence")
	}
	fixture.denyPods.Store(true)
	batch, err = reader.Current(t.Context())
	var failure *capability.SelectionError
	if !errors.As(err, &failure) || failure.Reason != capability.AccessDenied || len(batch.Pods) != 0 {
		t.Fatal("revoked required inventory retained data")
	}
}

func TestCurrentOutputAndRequestBudgets(t *testing.T) {
	for _, opts := range []Options{{MaxOutputBytes: 1}, {MaxTotalBytes: 1}} {
		reader, _ := newCurrentFixture(t, opts)
		batch, err := reader.Current(t.Context())
		if err == nil || len(batch.Pods) != 0 {
			t.Fatalf("unbounded output: %+v err=%v", batch, err)
		}
	}
}

func TestCurrentCancellationBoundsWholeRefreshAndQueuedReaders(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	reader, err := NewNamespace(&rest.Config{Host: server.URL}, "team-a", Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := reader.Current(ctx); done <- err }()
	<-entered
	queued, stop := context.WithCancel(t.Context())
	stop()
	if _, err := reader.Current(queued); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("refresh ignored cancellation")
	}
}

func TestCurrentConcurrentRefreshes(t *testing.T) {
	reader, _ := newCurrentFixture(t, Options{})
	var pending sync.WaitGroup
	for range 4 {
		pending.Add(1)
		go func() {
			defer pending.Done()
			batch, err := reader.Current(t.Context())
			if err != nil || len(batch.Pods) != 1 {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	pending.Wait()
}

func TestCurrentRequestBudgetPreservesRequiredInventory(t *testing.T) {
	reader, _ := newCurrentFixture(t, Options{MaxRequests: 1})
	batch, err := reader.Current(t.Context())
	if err != nil || len(batch.Pods) != 1 || batch.Completeness != capability.Partial || batch.Pods[0].WorkingSet.Bytes != nil {
		t.Fatalf("budgeted refresh: %v", err)
	}
}

func TestOptionalMetricTimeoutRetainsPodResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/namespaces/team-a/pods" {
			podListResponse(t, w, "", pendingPod("team-a", "app"))
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	reader, err := NewNamespace(&rest.Config{Host: server.URL}, "team-a", Options{Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	batch, err := reader.Current(t.Context())
	if err != nil || len(batch.Pods) != 1 || batch.Pods[0].WorkingSet.Bytes != nil || batch.Completeness != capability.Partial {
		t.Fatalf("optional timeout: %v", err)
	}
}
