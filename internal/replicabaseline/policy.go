package replicabaseline

type Metric string

const (
	Charge         Metric = "cgroup-charge"
	AnonFraction   Metric = "anon-fraction"
	FileFraction   Metric = "file-cache-fraction"
	ShmemFraction  Metric = "shmem-fraction"
	KernelFraction Metric = "kernel-fraction"
	Swap           Metric = "swap-bytes"
	ChargeSlope    Metric = "charge-slope"
	LimitUsage     Metric = "limit-usage"
	PSISome        Metric = "maximum-container-psi-some"
	PSIFull        Metric = "maximum-container-psi-full"
	OOMRate        Metric = "oom-events-per-second"
	OOMKillRate    Metric = "oom-kill-events-per-second"
	HighRate       Metric = "high-events-per-second"
	MaxRate        Metric = "max-events-per-second"
)

type direction string

const (
	bothDirections direction = "both"
	higherOnly     direction = "higher"
)

type metricPolicy struct {
	metric    Metric
	unit      string
	floor     float64
	direction direction
}

// These versioned effect floors are product heuristics, not safety thresholds
// or probabilities. Existing diagnoses and resource recommendations are separate.
var policies = []metricPolicy{
	{Charge, "bytes", 16 << 20, bothDirections},
	{AnonFraction, "fraction", .10, bothDirections},
	{FileFraction, "fraction", .10, bothDirections},
	{ShmemFraction, "fraction", .10, bothDirections},
	{KernelFraction, "fraction", .10, bothDirections},
	{Swap, "bytes", 16 << 20, higherOnly},
	{ChargeSlope, "bytes-per-second", (1 << 20) / 60.0, bothDirections},
	{LimitUsage, "fraction", .10, higherOnly},
	{PSISome, "percent", 1, higherOnly},
	{PSIFull, "percent", .1, higherOnly},
	{OOMRate, "events-per-second", 1.0 / 60, higherOnly},
	{OOMKillRate, "events-per-second", 1.0 / 60, higherOnly},
	{HighRate, "events-per-second", 1.0 / 60, higherOnly},
	{MaxRate, "events-per-second", 1.0 / 60, higherOnly},
}

func policyFor(metric Metric) (metricPolicy, bool) {
	for _, policy := range policies {
		if policy.metric == metric {
			return policy, true
		}
	}
	return metricPolicy{}, false
}
