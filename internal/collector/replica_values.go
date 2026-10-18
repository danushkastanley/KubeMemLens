package collector

import (
	"math"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

func replicaValues(containers []api.ContainerSnapshot, at time.Time) (map[replicabaseline.Metric]replicabaseline.Value, time.Time) {
	values := map[replicabaseline.Metric]replicabaseline.Value{}
	var charge, anon, file, shmem, kernel, swap uint64
	var some, full, limitUsage float64
	swapKnown, pressureKnown, limitKnown, ratesKnown := true, true, true, true
	rates := [4]float64{}
	deltaStart := containers[0].DeltaStartedAt
	span := at.Sub(deltaStart)
	ratesKnown = !deltaStart.IsZero() && span > 0 && span <= replicabaseline.FreshFor
	valid := true
	compositionKnown := [4]bool{true, true, true, true}
	add := func(total *uint64, n uint64) {
		if math.MaxUint64-*total < n {
			valid = false
			return
		}
		*total += n
	}
	for _, c := range containers {
		m := c.Memory
		for i, value := range []uint64{m.AnonBytes, m.FileCacheBytes(), m.ShmemBytes, m.KernelBytes} {
			compositionKnown[i] = compositionKnown[i] && value > 0
		}
		add(&charge, m.TotalBytes)
		add(&anon, m.AnonBytes)
		add(&file, m.FileCacheBytes())
		add(&shmem, m.ShmemBytes)
		add(&kernel, m.KernelBytes)
		add(&swap, m.SwapCurrentBytes)
		swapKnown = swapKnown && m.SwapCurrentKnown
		pressureKnown = pressureKnown && m.PressureKnown
		some = max(some, m.PSISomeAvg10)
		full = max(full, m.PSIFullAvg10)
		limitKnown = limitKnown && m.MaxKnown && !m.MaxUnlimited && m.MaxBytes > 0
		if m.MaxKnown && !m.MaxUnlimited && m.MaxBytes > 0 {
			limitUsage = max(limitUsage, float64(m.TotalBytes)/float64(m.MaxBytes))
		}
		// The legacy wire has no hierarchical event-file presence bit. Use
		// explicitly reported local counters; absence is not a measured zero.
		known := m.LocalEventsKnown && m.LocalEventDeltasKnown
		counts := [4]uint64{m.LocalOOMEventsDelta, m.LocalOOMKillEventsDelta, m.LocalHighEventsDelta, m.LocalMaxEventsDelta}
		ratesKnown = ratesKnown && known && c.DeltaWindowKnown && c.DeltaStartedAt.Equal(deltaStart)
		for i, count := range counts {
			rates[i] += float64(count)
		}
	}
	if !valid {
		return values, time.Time{}
	}
	available := func(metric replicabaseline.Metric, number float64) {
		values[metric] = replicabaseline.Value{State: replicabaseline.Available, Number: number}
	}
	available(replicabaseline.Charge, float64(charge))
	metrics := []replicabaseline.Metric{replicabaseline.AnonFraction, replicabaseline.FileFraction, replicabaseline.ShmemFraction, replicabaseline.KernelFraction}
	for i, bytes := range []uint64{anon, file, shmem, kernel} {
		// Legacy composition fields conflate absent keys and zero. Only
		// compare positive reported components from every contributor.
		// Independently sampled files can disagree: do not clamp fractions.
		if compositionKnown[i] && charge > 0 && bytes <= charge {
			available(metrics[i], float64(bytes)/float64(charge))
		}
	}
	if swapKnown {
		available(replicabaseline.Swap, float64(swap))
	}
	if pressureKnown {
		available(replicabaseline.PSISome, some)
		available(replicabaseline.PSIFull, full)
	}
	if limitKnown {
		available(replicabaseline.LimitUsage, limitUsage)
	}
	if !ratesKnown {
		return values, time.Time{}
	}
	for i, metric := range []replicabaseline.Metric{replicabaseline.OOMRate, replicabaseline.OOMKillRate, replicabaseline.HighRate, replicabaseline.MaxRate} {
		available(metric, rates[i]/span.Seconds())
	}
	return values, deltaStart
}
