package incidentsession

import (
	"bytes"
	"encoding/json"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"io"
)

// Capture documents have deeper, distinct schemas from the timeline. Bound
// structure before typed allocation; the schema decoder then validates members.
func boundedCaptureData(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	tokens := 0
	var value func(int, string) error
	value = func(depth int, field string) error {
		tokens++
		if depth > 16 || tokens > 8192 {
			return ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		if delim != '[' && delim != '{' {
			return ErrInvalid
		}
		count := 0
		limit := 128
		if delim == '[' && field == "points" {
			limit = memoryhistory.MaxPoints
		}
		for d.More() {
			count++
			if count > limit {
				return ErrInvalid
			}
			childField := field
			if delim == '{' {
				key, err := d.Token()
				text, ok := key.(string)
				childField = text
				if err != nil || !ok || len(text) > 256 {
					return ErrInvalid
				}
			}
			if err := value(depth+1, childField); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || (delim == '[' && end != json.Delim(']')) || (delim == '{' && end != json.Delim('}')) {
			return ErrInvalid
		}
		return nil
	}
	if value(0, "") != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
