package topologysource

import (
	"math"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func meminfo(data []byte, id uint16) (total, free *uint64, err error) {
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "Node" {
			return nil, nil, memorytopology.ErrInvalid
		}
		node, err := number(fields[1])
		if err != nil || node != uint64(id) {
			return nil, nil, memorytopology.ErrInvalid
		}
		name := strings.TrimSuffix(fields[2], ":")
		if seen[name] {
			return nil, nil, memorytopology.ErrInvalid
		}
		seen[name] = true
		if name != "MemTotal" && name != "MemFree" {
			continue
		}
		if len(fields) != 5 || fields[4] != "kB" {
			return nil, nil, memorytopology.ErrInvalid
		}
		n, err := number(fields[3])
		if err != nil || n > math.MaxUint64/1024 {
			return nil, nil, memorytopology.ErrInvalid
		}
		n *= 1024
		if name == "MemTotal" {
			total = &n
		} else {
			free = &n
		}
	}
	return total, free, nil
}

func counters(data []byte) (map[string]uint64, error) {
	result := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, memorytopology.ErrInvalid
		}
		if _, found := result[fields[0]]; found {
			return nil, memorytopology.ErrInvalid
		}
		n, err := number(fields[1])
		if err != nil {
			return nil, err
		}
		result[fields[0]] = n
	}
	return result, nil
}
func field(values map[string]uint64, name string) *uint64 {
	n, found := values[name]
	if !found {
		return nil
	}
	return &n
}
func placement(data []byte) (*memorytopology.Placement, error) {
	values, err := counters(data)
	if err != nil {
		return nil, err
	}
	return &memorytopology.Placement{HitPages: field(values, "numa_hit"), MissPages: field(values, "numa_miss"), ForeignPages: field(values, "numa_foreign"), LocalPages: field(values, "local_node"), OtherPages: field(values, "other_node"), InterleavePages: field(values, "interleave_hit")}, nil
}
