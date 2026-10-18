package incidentsession

import (
	"encoding/json"
	"time"
)

// The existing parser token/depth ceilings also constrain encoded session state.
// Reserve a closed representation before accepting an entry, just as add reserves
// terminal entry bytes. A full timeline must remain readable after it is closed.
func reserveExportSyntax(next *record, encoded []byte, now time.Time) error {
	if boundedSyntax(encoded) != nil {
		return ErrCapacity
	}
	if next.closed != nil {
		return nil
	}
	closed := *next
	entry := newEntry(next.principal, Input{Kind: Closed, Source: "operator"}, now, uint64(len(next.entries)+1))
	closed.entries = append(append([]Entry(nil), next.entries...), entry)
	at := now.UTC()
	closed.closed = &at
	data, err := json.Marshal(fullDocument(&closed))
	if err != nil || boundedSyntax(data) != nil {
		return ErrCapacity
	}
	return nil
}
