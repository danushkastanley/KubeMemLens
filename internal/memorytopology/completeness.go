package memorytopology

import "math"

func PageBytes(pages, size uint64) (uint64, bool) {
	if size == 0 || pages > math.MaxUint64/size {
		return 0, false
	}
	return pages * size, true
}

func PoolConsistent(p HugePool) bool {
	if p.TotalPages == nil || p.FreePages == nil || p.SurplusPages == nil {
		return false
	}
	if *p.FreePages > *p.TotalPages || *p.SurplusPages > *p.TotalPages {
		return false
	}
	if p.ReservedPages != nil && *p.ReservedPages > *p.FreePages {
		return false
	}
	_, ok := PageBytes(*p.TotalPages, p.PageSizeBytes)
	return ok
}

func NUMAComplete(nodes []NUMANode) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, n := range nodes {
		if n.TotalBytes == nil || n.FreeBytes == nil || *n.FreeBytes > *n.TotalBytes || !placementComplete(n.Placement) {
			return false
		}
		for _, p := range n.Pools {
			if !PoolConsistent(p) {
				return false
			}
		}
	}
	return true
}
func PoolsComplete(pools []HugePool) bool {
	if len(pools) == 0 {
		return false
	}
	for _, p := range pools {
		if !PoolConsistent(p) || p.ReservedPages == nil {
			return false
		}
	}
	return true
}
func CgroupsComplete(groups []HugeCgroup) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if g.CurrentBytes == nil || (g.Limit.Bytes == nil && !g.Limit.Unlimited) || g.LimitFailures == nil || g.NUMATotalBytes == nil || len(g.NUMA) == 0 {
			return false
		}
		total := uint64(0)
		for _, node := range g.NUMA {
			if math.MaxUint64-total < node.Bytes {
				return false
			}
			total += node.Bytes
		}
		if total != *g.NUMATotalBytes || total != *g.CurrentBytes {
			return false
		}
	}
	return true
}
func placementComplete(p *Placement) bool {
	return p != nil && p.HitPages != nil && p.MissPages != nil && p.ForeignPages != nil && p.LocalPages != nil && p.OtherPages != nil && p.InterleavePages != nil
}
