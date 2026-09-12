package kube

// VolumeHealthStats contains fixed aggregate cache counters. It provides no
// status, driver, caller or object identity and is not a health interpretation.
type VolumeHealthStats struct {
	Enabled                                   bool
	Entries                                   int
	Bytes                                     int
	PayloadWrites, Reads, Failures, Throttled uint64
}

func (r *volumeResolver) VolumeHealthStats() VolumeHealthStats {
	if r.health == nil {
		return VolumeHealthStats{}
	}
	s := r.health.stats()
	return VolumeHealthStats{Enabled: true, Entries: s.Entries, Bytes: s.Bytes,
		PayloadWrites: s.PayloadWrites, Reads: s.Reads, Failures: s.Failures, Throttled: s.Throttled}
}
