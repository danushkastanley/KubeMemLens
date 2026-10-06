package volumecontext

import "time"

// NewSourceBatch validates a complete acquisition, then omits measurements
// already past expiry. Independently cached source records need not have the
// same age. The omission count is aggregate-only; retained times are unchanged.
// Incoming private batches must use NewBatch, which still rejects expired rows.
func NewSourceBatch(nodeName, nodeUID string, at time.Time, state Usage, records []RawUsage, now time.Time) (Batch, int, error) {
	batch, err := newValidatedBatch(nodeName, nodeUID, at, state, records, now)
	if err != nil {
		return Batch{}, 0, err
	}
	cutoff := now.Add(-ExpireAfter)
	current := batch.records[:0]
	expired := 0
	for _, row := range batch.records {
		if row.Filesystem.CapturedAt.Before(cutoff) {
			expired++
			continue
		}
		current = append(current, row)
	}
	if expired != 0 {
		// Do not retain expired values or an oversized backing array behind a
		// smaller record count used by the store's retention accounting.
		batch.records = make([]RawUsage, len(current))
		copy(batch.records, current)
	}
	return batch, expired, nil
}
