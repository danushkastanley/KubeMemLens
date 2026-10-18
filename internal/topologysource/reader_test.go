package topologysource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func fixture(t *testing.T) (*Reader, Paths) {
	t.Helper()
	base := t.TempDir()
	paths := Paths{System: filepath.Join(base, "system"), Memory: filepath.Join(base, "memory"), Cgroup: filepath.Join(base, "cgroup")}
	for _, path := range []string{paths.System, paths.Memory, paths.Cgroup} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	r, err := New(paths, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return r, paths
}
func put(t *testing.T, root, path, data string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func completeFixture(t *testing.T) (*Reader, Paths) {
	t.Helper()
	r, p := fixture(t)
	put(t, p.System, "node/online", "0-1\n")
	for _, id := range []string{"0", "1"} {
		prefix := "node/node" + id + "/"
		put(t, p.System, prefix+"meminfo", "Node "+id+" MemTotal: 1048576 kB\nNode "+id+" MemFree: 524288 kB\nNode "+id+" HugePages_Total: 4\n")
		put(t, p.System, prefix+"numastat", "numa_hit 1\nnuma_miss 2\nnuma_foreign 3\ninterleave_hit 4\nlocal_node 5\nother_node 6\n")
		for name, value := range map[string]string{"nr_hugepages": "4", "free_hugepages": "3", "surplus_hugepages": "1"} {
			put(t, p.System, prefix+"hugepages/hugepages-2048kB/"+name, value)
		}
	}
	for name, value := range map[string]string{"nr_hugepages": "8", "free_hugepages": "6", "surplus_hugepages": "2", "resv_hugepages": "1"} {
		put(t, p.Memory, "hugepages/hugepages-2048kB/"+name, value)
	}
	for name, value := range map[string]string{"current": "4194304", "max": "max", "rsvd.current": "6291456", "rsvd.max": "16777216", "events": "max 1\n", "numa_stat": "total=4194304 N0=2097152 N1=2097152\n"} {
		put(t, p.Cgroup, "hugetlb.2MB."+name, value)
	}
	return r, p
}
func read(t *testing.T, r *Reader) memorytopology.Observation {
	t.Helper()
	o, err := r.Read(context.Background(), "node-a", "uid-a")
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func TestReadComplete(t *testing.T) {
	r, _ := completeFixture(t)
	o := read(t, r)
	if o.NUMA.Completeness != capability.Complete || o.Pools.Completeness != capability.Complete || o.Cgroup.Completeness != capability.Complete {
		t.Fatalf("incomplete: %+v", o)
	}
	if *o.NUMA.Items[0].TotalBytes != 1<<30 || *o.NUMA.Items[0].Placement.MissPages != 2 {
		t.Fatal("units changed")
	}
	p := o.Pools.Items[0]
	g := o.Cgroup.Items[0]
	if *p.TotalPages != 8 || *p.SurplusPages != 2 || *p.ReservedPages != 1 || *g.CurrentBytes != 4<<20 || *g.ReservationBytes != 6<<20 || !g.Limit.Unlimited || *g.ReservationLimit.Bytes != 16<<20 || *g.LimitFailures != 1 || g.NUMA[0].Bytes != 2<<20 {
		t.Fatalf("source quantities changed: %+v %+v", p, g)
	}
	if o.NUMA.Items[0].Pools[0].ReservedPages != nil {
		t.Fatal("per-node reservation invented")
	}
}
func TestReadMissingAndZero(t *testing.T) {
	r, p := fixture(t)
	o := read(t, r)
	if o.NUMA.Availability != capability.Unsupported || o.Pools.Availability != capability.Unsupported || o.Cgroup.Availability != capability.Unsupported {
		t.Fatalf("missing became available: %+v", o)
	}
	for _, name := range []string{"nr_hugepages", "free_hugepages", "surplus_hugepages", "resv_hugepages"} {
		put(t, p.Memory, "hugepages/hugepages-2048kB/"+name, "0")
	}
	o = read(t, r)
	if o.Pools.Completeness != capability.Complete || *o.Pools.Items[0].TotalPages != 0 || o.Cgroup.Availability != capability.Unsupported {
		t.Fatal("zero pool or missing root cgroup misreported")
	}
	put(t, p.Cgroup, "hugetlb.2MB.max", "0")
	o = read(t, r)
	if o.Cgroup.Completeness != capability.Partial || o.Cgroup.Items[0].CurrentBytes != nil || *o.Cgroup.Items[0].Limit.Bytes != 0 {
		t.Fatal("missing current became zero")
	}
}
func TestSourceFailureIsolation(t *testing.T) {
	for _, test := range []struct {
		name, path, data string
		reason           memorytopology.Reason
	}{
		{"malformed", "node/node0/meminfo", "Node 0 MemTotal: -1 kB", memorytopology.InvalidSource},
		{"wrong-node", "node/node0/meminfo", "Node 1 MemTotal: 1 kB", memorytopology.InvalidSource},
		{"oversized", "node/node0/meminfo", strings.Repeat("x", MaxFileBytes+1), memorytopology.SourceBounds},
		{"too-many-nodes", "node/online", "0-8", memorytopology.SourceBounds},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, p := completeFixture(t)
			put(t, p.System, test.path, test.data)
			o := read(t, r)
			if o.NUMA.Reason != test.reason || o.Pools.Completeness != capability.Complete || o.Cgroup.Completeness != capability.Complete {
				t.Fatalf("failure not isolated: %+v", o)
			}
		})
	}
}
func TestMissingFieldsRemainPartial(t *testing.T) {
	r, p := completeFixture(t)
	if err := os.Remove(filepath.Join(p.System, "node/node1/meminfo")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.Cgroup, "hugetlb.2MB.events")); err != nil {
		t.Fatal(err)
	}
	o := read(t, r)
	if o.NUMA.Completeness != capability.Partial || o.NUMA.Items[1].TotalBytes != nil || o.Cgroup.Completeness != capability.Partial || o.Cgroup.Items[0].LimitFailures != nil {
		t.Fatal("missing fields presented as complete")
	}
}
func TestRootPreventsSymlinkEscape(t *testing.T) {
	r, p := completeFixture(t)
	outside := t.TempDir()
	put(t, outside, "secret", "Node 0 MemTotal: 1234 kB\nNode 0 MemFree: 1234 kB\n")
	path := filepath.Join(p.System, "node/node0/meminfo")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), path); err != nil {
		t.Fatal(err)
	}
	o := read(t, r)
	if o.NUMA.Availability != capability.Unavailable || len(o.NUMA.Items) != 0 || o.Pools.Completeness != capability.Complete {
		t.Fatal("escaped rooted source")
	}
}
func TestCancellationAndOverlap(t *testing.T) {
	r, _ := completeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o, err := r.Read(ctx, "node-a", "uid-a")
	if err != nil || o.NUMA.Reason != memorytopology.TimedOut || o.Pools.Reason != memorytopology.TimedOut {
		t.Fatalf("cancellation lost: %+v %v", o, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err = r.Read(context.Background(), "node-a", "uid-a"); !errors.Is(err, ErrBusy) {
		t.Fatalf("overlap admitted: %v", err)
	}
}
func TestInvalidPathsAndIdentity(t *testing.T) {
	if _, err := New(Paths{System: "relative"}, nil); !errors.Is(err, memorytopology.ErrInvalid) {
		t.Fatal("relative root allowed")
	}
	r, _ := fixture(t)
	if _, err := r.Read(context.Background(), "../node", "uid"); !errors.Is(err, memorytopology.ErrInvalid) {
		t.Fatal("invalid identity allowed")
	}
}

func TestFiniteLimitKeepsKernelByteValue(t *testing.T) {
	r, p := completeFixture(t)
	const kernelLimit = "9223372036854771712"
	put(t, p.Cgroup, "hugetlb.2MB.max", kernelLimit)
	put(t, p.Cgroup, "hugetlb.2MB.rsvd.max", kernelLimit)
	o := read(t, r)
	g := o.Cgroup.Items[0]
	if o.Cgroup.Completeness != capability.Complete || g.Limit.Unlimited || *g.Limit.Bytes != 9223372036854771712 || *g.ReservationLimit.Bytes != 9223372036854771712 {
		t.Fatal("finite kernel limit was rejected or rounded")
	}
	put(t, p.Cgroup, "hugetlb.2MB.current", "1")
	if got := read(t, r); got.Cgroup.Reason != memorytopology.InvalidSource {
		t.Fatal("unaligned charge accepted")
	}
}
