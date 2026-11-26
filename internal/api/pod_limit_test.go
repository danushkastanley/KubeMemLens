package api

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Without a finite Pod cgroup limit the effective limit is exactly today's
// container-sum value, with the same ratio and risk. With one, the Pod cgroup
// decides, and "unlimited" containers no longer make the Pod unlimited.
func FuzzPodEffectiveLimit(f *testing.F) {
	const mib = 1 << 20
	// Pod-level resources: one container limited to 64Mi, one unlimited,
	// and a 256Mi Pod cgroup limit using 240Mi including the sandbox.
	f.Add(uint64(200*mib), uint64(64*mib), true, true, uint64(240*mib), true, uint64(256*mib), true, false)
	f.Add(uint64(200*mib), uint64(256*mib), true, false, uint64(0), false, uint64(256*mib), true, false)
	f.Add(uint64(10), uint64(0), false, false, uint64(0), false, uint64(0), true, true)
	f.Add(uint64(95), uint64(100), true, false, uint64(0), false, uint64(0), false, false)
	f.Fuzz(func(t *testing.T, total, sumMax uint64, sumKnown, sumUnlimited bool, current uint64, currentKnown bool, max uint64, maxKnown, maxUnlimited bool) {
		pod := PodSnapshot{Memory: model.MemoryBreakdown{TotalBytes: total, MaxBytes: sumMax, MaxKnown: sumKnown, MaxUnlimited: sumUnlimited}}
		pod.PodCgroup = model.PodCgroupMemory{CurrentKnown: currentKnown, MaxKnown: maxKnown, MaxUnlimited: maxUnlimited}
		if currentKnown {
			pod.PodCgroup.CurrentBytes = current
		}
		if maxKnown && !maxUnlimited {
			pod.PodCgroup.MaxBytes = max
		}
		limit := pod.EffectiveLimit()
		if !maxKnown || maxUnlimited || max == 0 {
			want := PodLimit{Source: PodLimitContainerSum, Known: sumKnown, Unlimited: sumUnlimited, LimitBytes: sumMax, UsedBytes: total}
			if limit != want || limit.AtRisk() != pod.Memory.HasLimitRisk() || limit.UsageRatio() != pod.Memory.LimitUsageRatio() {
				t.Fatalf("fallback %#v differs from the container sum %#v", limit, want)
			}
			return
		}
		used := total
		if currentKnown {
			used = current
		}
		if limit.Source != PodLimitPodCgroup || !limit.Known || limit.Unlimited || limit.LimitBytes != max || limit.UsedBytes != used {
			t.Fatalf("Pod cgroup limit %#v, want %d of %d", limit, used, max)
		}
		if limit.AtRisk() != (float64(used)/float64(max) >= 0.90) {
			t.Fatalf("risk %t for %d of %d", limit.AtRisk(), used, max)
		}
	})
}
