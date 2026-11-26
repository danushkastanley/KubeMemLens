package cgroup

import "github.com/danushkastanley/kube-memlens/internal/model"

// zswapFromStat is known only when memory.stat has both keys: one without the
// other cannot describe the pool and what it holds.
func zswapFromStat(stat MemoryStat) model.Zswap {
	pool, poolKnown := stat.Values["zswap"]
	swapped, swappedKnown := stat.Values["zswapped"]
	if !poolKnown || !swappedKnown {
		return model.Zswap{}
	}
	return model.Zswap{Known: true, PoolBytes: pool, SwappedBytes: swapped}
}

// groupKillEvents reads oom_group_kill from parsed memory.events or
// memory.events.local values. It is known only when the key is present.
func groupKillEvents(values map[string]uint64) (uint64, bool) {
	value, known := values["oom_group_kill"]
	return value, known
}
