package metrics

type Options struct {
	Enabled bool

	IncludeNamespaceMetrics bool
	IncludePodMetrics       bool
	IncludeContainerMetrics bool

	IncludeDiagnosisMetrics bool
	IncludeEventMetrics     bool

	MaxPods       int
	MaxContainers int
}

func DefaultOptions() Options {
	return Options{
		Enabled:                 true,
		IncludeNamespaceMetrics: true,
		IncludePodMetrics:       true,
		IncludeContainerMetrics: false,
		IncludeDiagnosisMetrics: true,
		IncludeEventMetrics:     true,
		MaxPods:                 2000,
		MaxContainers:           5000,
	}
}
