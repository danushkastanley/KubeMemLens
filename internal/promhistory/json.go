package promhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

// Check structure before typed allocation, including ignored future fields.
// Duplicate/case-aliased keys cannot replace identity or boundary fields.
func boundedJSON(ctx context.Context, data []byte) error {
	if !utf8.Valid(data) {
		return memoryhistory.ErrSource
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := readValue(ctx, d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return memoryhistory.ErrSource
	}
	return nil
}

func readValue(ctx context.Context, d *json.Decoder, field string, depth int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if depth > 8 {
		return memoryhistory.ErrSource
	}
	token, err := d.Token()
	if err != nil {
		return memoryhistory.ErrSource
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if s, ok := token.(string); ok && len(s) > 1024 {
			return memoryhistory.ErrSource
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || len(name) > 128 || len(seen) >= 32 {
				return memoryhistory.ErrSource
			}
			folded := strings.ToLower(name)
			// Prometheus label maps are case-sensitive; struct field aliases are not.
			if strings.EqualFold(field, "metric") {
				folded = name
			}
			if seen[folded] {
				return memoryhistory.ErrSource
			}
			seen[folded] = true
			if err := readValue(ctx, d, name, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 32
		switch field {
		case "result":
			limit = 2 * memoryhistory.MaxTargets
		case "values":
			limit = memoryhistory.MaxPoints
		case "":
			limit = 2
		}
		for n := 0; d.More(); n++ {
			if n >= limit {
				return memoryhistory.ErrSource
			}
			if err := readValue(ctx, d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return memoryhistory.ErrSource
	}
	_, err = d.Token()
	if err != nil {
		return memoryhistory.ErrSource
	}
	return nil
}
