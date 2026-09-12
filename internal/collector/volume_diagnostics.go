package collector

// VolumeUsageStats contains aggregate in-memory retention, never tenant identity.
// Commits counts accepted entry replacements, including failed-source reports;
// it does not count Kubernetes writes or disk persistence.
type VolumeUsageStats struct {
	Entries int
	Bytes   int
	Commits uint64
}

func (s *Store) VolumeUsageStats() VolumeUsageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return VolumeUsageStats{Entries: len(s.volumes.entries), Bytes: s.volumes.bytes, Commits: s.volumes.commits}
}
