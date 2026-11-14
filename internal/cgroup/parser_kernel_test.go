package cgroup

import (
	"strings"
	"testing"
	"testing/fstest"
)

func statFS(lines ...string) fstest.MapFS {
	return fstest.MapFS{
		"memory.current": {Data: []byte("1048576\n")},
		"memory.stat":    {Data: []byte(strings.Join(lines, "\n") + "\n")},
	}
}

// memory.stat gained the kernel key in Linux 5.18. Older kernels report slab
// and page tables but no kernel total, which must not read as zero kernel memory.
func TestKernelTotalIsUnreportedWhenMemoryStatHasNoKernelKey(t *testing.T) {
	tests := map[string]struct {
		stat     []string
		reported bool
		kernel   uint64
	}{
		"Linux 5.15: slab and page tables, no kernel key": {
			[]string{"anon 4096", "file 8192", "slab 2048", "pagetables 1024"}, false, 0},
		"Linux 5.15: only the slab parts": {
			[]string{"anon 4096", "slab_reclaimable 512", "slab_unreclaimable 512"}, false, 0},
		"Linux 5.18 and later": {
			[]string{"anon 4096", "kernel 6144", "slab 2048", "pagetables 1024"}, true, 6144},
		"kernel key present and zero": {
			[]string{"anon 4096", "kernel 0", "slab 0", "pagetables 0"}, true, 0},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			memory, err := ParseFS("container", statFS(test.stat...))
			if err != nil {
				t.Fatal(err)
			}
			if memory.KernelReported() != test.reported || memory.KernelBytes != test.kernel {
				t.Fatalf("KernelReported=%t KernelBytes=%d, want %t and %d",
					memory.KernelReported(), memory.KernelBytes, test.reported, test.kernel)
			}
		})
	}
}
