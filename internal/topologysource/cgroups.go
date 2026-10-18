package topologysource

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func (r *Reader) cgroups(b *readBudget, sizes []uint64) memorytopology.Section[memorytopology.HugeCgroup] {
	if err := b.ctx.Err(); err != nil {
		return failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, err)
	}
	root, err := os.OpenRoot(r.paths.Cgroup)
	if err != nil {
		return failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, err)
	}
	defer root.Close()
	groups := make([]memorytopology.HugeCgroup, 0, len(sizes))
	present := false
	for _, size := range sizes {
		group, found, err := b.cgroup(root, size)
		if err != nil {
			return failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, err)
		}
		present = present || found
		groups = append(groups, group)
	}
	if !present {
		return failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, fs.ErrNotExist)
	}
	return section(memorytopology.HugeTLBCgroup, groups, memorytopology.CgroupsComplete(groups), r.now().UTC(), nil)
}

// Kernel HugeTLB cgroup names use the largest exactly divisible binary unit.
func pageName(size uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for size%1024 == 0 && i < len(units)-1 {
		size /= 1024
		i++
	}
	return strconv.FormatUint(size, 10) + units[i]
}

func (b *readBudget) cgroup(root *os.Root, size uint64) (memorytopology.HugeCgroup, bool, error) {
	g := memorytopology.HugeCgroup{PageSizeBytes: size, NUMA: []memorytopology.DomainBytes{}}
	prefix := "hugetlb." + pageName(size) + "."
	present := false
	for _, name := range []string{"current", "max", "rsvd.current", "rsvd.max", "events", "numa_stat"} {
		data, err := b.read(root, prefix+name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return g, present, err
		}
		present = true
		switch name {
		case "current", "rsvd.current":
			var n uint64
			n, err = number(strings.TrimSpace(string(data)))
			if name == "current" {
				g.CurrentBytes = &n
			} else {
				g.ReservationBytes = &n
			}
		case "max":
			g.Limit, err = parseLimit(data)
		case "rsvd.max":
			g.ReservationLimit, err = parseLimit(data)
		case "events":
			var values map[string]uint64
			values, err = counters(data)
			g.LimitFailures = field(values, "max")
		case "numa_stat":
			g.NUMATotalBytes, g.NUMA, err = numaBytes(data)
		}
		if err != nil {
			return g, present, err
		}
	}
	for _, value := range []*uint64{g.CurrentBytes, g.ReservationBytes, g.NUMATotalBytes} {
		if value != nil && *value%size != 0 {
			return g, present, memorytopology.ErrInvalid
		}
	}
	for _, node := range g.NUMA {
		if node.Bytes%size != 0 {
			return g, present, memorytopology.ErrInvalid
		}
	}
	return g, present, nil
}

func parseLimit(data []byte) (memorytopology.Limit, error) {
	text := strings.TrimSpace(string(data))
	if text == "max" {
		return memorytopology.Limit{Unlimited: true}, nil
	}
	n, err := number(text)
	return memorytopology.Limit{Bytes: &n}, err
}

func numaBytes(data []byte) (*uint64, []memorytopology.DomainBytes, error) {
	fields := strings.Fields(string(data))
	if len(fields) > memorytopology.MaxNodes+1 {
		return nil, nil, memorytopology.ErrBounds
	}
	var total *uint64
	nodes := []memorytopology.DomainBytes{}
	seen := map[uint16]bool{}
	for _, token := range fields {
		key, value, ok := strings.Cut(token, "=")
		n, err := number(value)
		if !ok || err != nil {
			return nil, nil, memorytopology.ErrInvalid
		}
		if key == "total" {
			if total != nil {
				return nil, nil, memorytopology.ErrInvalid
			}
			total = &n
			continue
		}
		id, err := number(strings.TrimPrefix(key, "N"))
		if !strings.HasPrefix(key, "N") || err != nil || id > 65535 || seen[uint16(id)] {
			return nil, nil, memorytopology.ErrInvalid
		}
		seen[uint16(id)] = true
		nodes = append(nodes, memorytopology.DomainBytes{ID: uint16(id), Bytes: n})
	}
	if total == nil {
		return nil, nil, memorytopology.ErrInvalid
	}
	return total, nodes, nil
}
