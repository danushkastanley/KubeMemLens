package topologysource

import (
	"cmp"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func (b *readBudget) pools(root *os.Root, path string, perNode bool) ([]memorytopology.HugePool, error) {
	entries, err := b.entries(root, path)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
	pools := []memorytopology.HugePool{}
	seen := map[uint64]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "hugepages-") {
			continue
		}
		if !entry.IsDir() || !strings.HasSuffix(name, "kB") {
			return nil, memorytopology.ErrInvalid
		}
		kb, err := number(strings.TrimSuffix(strings.TrimPrefix(name, "hugepages-"), "kB"))
		if err != nil || kb > math.MaxUint64/1024 {
			return nil, memorytopology.ErrInvalid
		}
		size := kb * 1024
		if size < 4096 || size > 1<<50 || size&(size-1) != 0 || seen[size] {
			return nil, memorytopology.ErrInvalid
		}
		seen[size] = true
		if len(pools) == memorytopology.MaxPageSizes {
			return nil, memorytopology.ErrBounds
		}
		pool := memorytopology.HugePool{PageSizeBytes: size}
		prefix := path + "/" + name + "/"
		for _, field := range []struct {
			name   string
			target **uint64
		}{{"nr_hugepages", &pool.TotalPages}, {"free_hugepages", &pool.FreePages}, {"surplus_hugepages", &pool.SurplusPages}} {
			*field.target, err = b.scalar(root, prefix+field.name)
			if err != nil {
				return nil, err
			}
		}
		if !perNode {
			pool.ReservedPages, err = b.scalar(root, prefix+"resv_hugepages")
			if err != nil {
				return nil, err
			}
		}
		pools = append(pools, pool)
	}
	slices.SortFunc(pools, func(a, b memorytopology.HugePool) int { return cmp.Compare(a.PageSizeBytes, b.PageSizeBytes) })
	return pools, nil
}
func pageSizes(o memorytopology.Observation) []uint64 {
	sizes := map[uint64]bool{}
	for _, pool := range o.Pools.Items {
		sizes[pool.PageSizeBytes] = true
	}
	for _, node := range o.NUMA.Items {
		for _, pool := range node.Pools {
			sizes[pool.PageSizeBytes] = true
		}
	}
	result := make([]uint64, 0, len(sizes))
	for size := range sizes {
		result = append(result, size)
	}
	slices.Sort(result)
	return result
}
