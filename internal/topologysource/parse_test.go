package topologysource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func TestCgroupUnits(t *testing.T) {
	for size, want := range map[uint64]string{4096: "4KB", 65536: "64KB", 2 << 20: "2MB", 32 << 20: "32MB", 1 << 30: "1GB", 1 << 50: "1PB"} {
		if got := pageName(size); got != want {
			t.Fatalf("%d: %s != %s", size, got, want)
		}
	}
}
func TestNUMABytesValidation(t *testing.T) {
	for _, text := range []string{"", "N0=0", "total=0 total=0", "total=0 N0=0 N0=0", "total=0 N65536=0", "total=-1", "total=0 x=0", "total=0 N0=1=2", "total=0 N0=0 N1=0 N2=0 N3=0 N4=0 N5=0 N6=0 N7=0 N8=0"} {
		t.Run(text, func(t *testing.T) {
			if _, _, err := numaBytes([]byte(text)); err == nil {
				t.Fatal("invalid data accepted")
			}
		})
	}
	total, nodes, err := numaBytes([]byte("total=2097152 N0=2097152 N1=0\n"))
	if err != nil || *total != 2<<20 || nodes[0].Bytes != 2<<20 {
		t.Fatal("bytes interpreted as pages")
	}
}
func TestNumericAndListValidation(t *testing.T) {
	for _, text := range []string{"-1", "+1", "18446744073709551616", "1 2", ""} {
		if _, err := number(text); err == nil {
			t.Fatalf("invalid number %q", text)
		}
	}
	for _, text := range []string{"", "1-0", "0,0", "0-8", "65536", "0-1-2", "0,1-2,2"} {
		if _, err := nodeIDs(text); err == nil {
			t.Fatalf("invalid nodes %q", text)
		}
	}
	for _, text := range []string{"Node 0 MemTotal: 1 MB", "Node 0 MemTotal: 18446744073709551615 kB", "Node 0 MemFree: 1 kB\nNode 0 MemFree: 1 kB"} {
		if _, _, err := meminfo([]byte(text), 0); err == nil {
			t.Fatalf("invalid meminfo %q", text)
		}
	}
	if _, err := counters([]byte("max 1\nmax 2")); err == nil {
		t.Fatal("duplicate counter accepted")
	}
}
func TestReadBounds(t *testing.T) {
	_, p := fixture(t)
	put(t, p.Memory, "value", strings.Repeat("1", MaxFileBytes))
	root, err := os.OpenRoot(p.Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, b := range []*readBudget{{ctx: context.Background(), files: MaxFiles}, {ctx: context.Background(), bytes: MaxReadBytes}} {
		if _, err := b.read(root, "value"); !errors.Is(err, memorytopology.ErrBounds) {
			t.Fatalf("budget exceeded: %v", err)
		}
	}
	for i := 0; i <= MaxDirectoryEntries; i++ {
		put(t, p.Memory, fmt.Sprintf("hugepages/entry%d", i), "")
	}
	b := &readBudget{ctx: context.Background()}
	if _, err := b.entries(root, "hugepages"); !errors.Is(err, memorytopology.ErrBounds) {
		t.Fatalf("directory budget exceeded: %v", err)
	}
}
func TestPoolAndCgroupMalformedSources(t *testing.T) {
	for _, test := range []struct{ name, path, data string }{
		{"negative-current", "hugetlb.2MB.current", "-1"},
		{"duplicate-event", "hugetlb.2MB.events", "max 0\nmax 1"},
		{"bad-limit", "hugetlb.2MB.max", "unlimited"},
		{"bad-node", "hugetlb.2MB.numa_stat", "total=0 N0=0 N0=0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, p := completeFixture(t)
			put(t, p.Cgroup, test.path, test.data)
			o := read(t, r)
			if o.Cgroup.Reason != memorytopology.InvalidSource || o.Pools.Items[0].TotalPages == nil {
				t.Fatal("malformed cgroup did not fail independently")
			}
		})
	}
}
