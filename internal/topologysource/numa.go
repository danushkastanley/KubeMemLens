package topologysource

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

func (b *readBudget) nodes(root *os.Root) ([]memorytopology.NUMANode, error) {
	data, err := b.read(root, "node/online")
	if err != nil {
		return nil, err
	}
	ids, err := nodeIDs(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, err
	}
	nodes := []memorytopology.NUMANode{}
	for _, id := range ids {
		node := memorytopology.NUMANode{ID: id, Pools: []memorytopology.HugePool{}}
		prefix := "node/node" + strconv.Itoa(int(id))
		data, err = b.read(root, prefix+"/meminfo")
		if err == nil {
			node.TotalBytes, node.FreeBytes, err = meminfo(data, id)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		data, err = b.read(root, prefix+"/numastat")
		if err == nil {
			node.Placement, err = placement(data)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		node.Pools, err = b.pools(root, prefix+"/hugepages", true)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	data, err = b.read(root, "node/online")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, errChanged
		}
		return nil, err
	}
	after, err := nodeIDs(strings.TrimSpace(string(data)))
	if err != nil || !slices.Equal(ids, after) {
		return nil, errChanged
	}
	return nodes, nil
}

func nodeIDs(text string) ([]uint16, error) {
	if text == "" {
		return nil, memorytopology.ErrInvalid
	}
	ids := []uint16{}
	seen := map[uint16]bool{}
	for _, part := range strings.Split(text, ",") {
		pair := strings.Split(part, "-")
		if len(pair) > 2 {
			return nil, memorytopology.ErrInvalid
		}
		first, err := number(pair[0])
		if err != nil || first > 65535 {
			return nil, memorytopology.ErrInvalid
		}
		last := first
		if len(pair) == 2 {
			last, err = number(pair[1])
			if err != nil || last > 65535 || last < first {
				return nil, memorytopology.ErrInvalid
			}
		}
		if last-first+1 > memorytopology.MaxNodes || uint64(len(ids))+last-first+1 > memorytopology.MaxNodes {
			return nil, memorytopology.ErrBounds
		}
		for n := first; n <= last; n++ {
			id := uint16(n)
			if seen[id] {
				return nil, memorytopology.ErrInvalid
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
}
