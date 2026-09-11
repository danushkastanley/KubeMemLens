package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type deepQueryFixture struct {
	SnapshotReader
	pod api.PodSnapshot
	err error
}

func (f deepQueryFixture) Pods(context.Context) ([]api.PodSnapshot, error) {
	return []api.PodSnapshot{f.pod}, f.err
}
func (f deepQueryFixture) Namespaces(context.Context) ([]api.NamespaceSnapshot, error) {
	return []api.NamespaceSnapshot{{Namespace: "team-a", Memory: f.pod.Memory}}, nil
}
func (f deepQueryFixture) Workloads(context.Context) ([]api.WorkloadSnapshot, error) {
	return []api.WorkloadSnapshot{{Namespace: "team-a", Kind: "Pod", Name: "app", Memory: f.pod.Memory}}, nil
}
func (f deepQueryFixture) Nodes(context.Context) ([]api.NodeSnapshotStatus, error) {
	return []api.NodeSnapshotStatus{{NodeName: "node-a", ContainerCount: 1}}, nil
}

func TestDeepCurrentPreservesExistingContracts(t *testing.T) {
	pod := api.PodSnapshot{Namespace: "team-a", PodName: "app", PodUID: "uid", CapturedAt: time.Now(), Freshness: api.EvidenceFreshnessStale, Completeness: api.EvidencePartial, Memory: model.MemoryBreakdown{TotalBytes: 42, AnonBytes: 17}}
	reader := deepObservationReader{reader: deepQueryFixture{pod: pod}}
	batch, err := reader.Current(t.Context())
	if err != nil || batch.Mode != capability.Deep || len(batch.Pods) != 1 {
		t.Fatalf("%+v %v", batch, err)
	}
	roundtrip, ok := batch.Pods[0].DeepSnapshot()
	if !ok || !reflect.DeepEqual(roundtrip, pod) {
		t.Fatal("deep contract changed")
	}
	if batch.Pods[0].WorkingSet.Bytes != nil || batch.Nodes[0].WorkingSet.Bytes != nil || batch.Nodes[0].DeepStatus.ContainerCount != 1 || batch.Namespaces[0].Cgroup.Memory.TotalBytes != 42 || batch.Workloads[0].Cgroup.Memory.AnonBytes != 17 {
		t.Fatal("mixed deep evidence and working sets")
	}
	cause := errors.New("scoped read failed")
	reader.reader = deepQueryFixture{err: cause}
	batch, err = reader.Current(t.Context())
	if !errors.Is(err, cause) || len(batch.Pods) != 0 {
		t.Fatal("lost required deep read failure")
	}
}
