package changemarkers

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (r *Report) UnmarshalJSON(data []byte) error {
	type wire Report
	var value wire
	if err := decode(data, MaxReportBytes, &value); err != nil {
		return err
	}
	*r = Report(value)
	return nil
}

func (c *Context) UnmarshalJSON(data []byte) error {
	type wire Context
	var value wire
	if err := decode(data, memoryhistory.MaxResponseBytes, &value); err != nil {
		return err
	}
	*c = Context(value)
	return nil
}

func decode(data []byte, limit int, target any) error {
	if len(data) > limit || !utf8.Valid(data) {
		return ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := value(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

func value(d *json.Decoder, field string, depth int) error {
	if depth > 10 {
		return ErrBounds
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if text, ok := token.(string); ok && len(text) > 512 {
			return ErrBounds
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 64 || len(seen) >= 24 {
				return ErrBounds
			}
			folded := strings.ToLower(key)
			if seen[folded] {
				return ErrInvalid
			}
			seen[folded] = true
			if err := value(d, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "markers":
			limit = MaxMarkers
		case "owners":
			limit = MaxOwners
		case "targets", "series":
			limit = memoryhistory.MaxTargets
		case "points":
			limit = memoryhistory.MaxPoints
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return ErrBounds
			}
			if err := value(d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
