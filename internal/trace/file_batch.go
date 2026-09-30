package trace

// MaxFileBatch bounds synchronous handoff of observations already available to
// an adapter. It is not a queue or a reason to wait for more observations.
const MaxFileBatch = 16

// FileBatchOutput optionally accepts 1..MaxFileBatch ordered observations in
// one synchronous call. Implementations retain no observations after returning,
// preserve per-event validation and limits, and stop on the first error. The
// caller must not retry any part of a failed batch.
type FileBatchOutput interface {
	FileActivities([]FileActivity) error
}
