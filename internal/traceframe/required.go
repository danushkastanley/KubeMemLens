package traceframe

// Nullable observations still require their keys: unknown is explicitly null,
// never a missing count silently interpreted as measured zero.
func requiredFields(object string) string {
	switch object {
	case "root":
		return "version|type"
	case "metadata":
		return "sessionID|engineDigest|programmeDigest|kind|target|paths|bounds|sessionStartedAt|deadline"
	case "target":
		return "namespace|pod|podUID|container|containerStartedAt|bindingDigest"
	case "bounds":
		return "durationNanos|events|outputBytes|mapBytes|pathBytes"
	case "event":
		return "observedAt"
	case "file":
		return "operation|requestedBytes|completedBytes"
	case "cache":
		return "operation|pages"
	case "oom":
		return "scope|victimPID|command"
	case "summary":
		return "sessionEndedAt|observationStartedAt|observationEndedAt|termination|engineCounts|writtenEvents|rejectedEvents|writtenBytesBeforeSummary|incomplete"
	case "engineCounts":
		return "produced|sampled|lost|rejected"
	default:
		return "" // Unknown object placement is rejected by the typed decoder.
	}
}
