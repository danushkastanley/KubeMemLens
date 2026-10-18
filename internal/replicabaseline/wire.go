package replicabaseline

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

func (r *Report) UnmarshalJSON(data []byte) error {
	if len(data) > MaxResponseBytes || !utf8.Valid(data) {
		return ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := replicaJSONValue(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	type wire Report
	var result wire
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return ErrInvalid
	}
	*r = Report(result)
	return nil
}

func replicaJSONValue(d *json.Decoder, field string, depth int) error {
	if depth > 8 {
		return ErrBounds
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if s, ok := token.(string); ok && len(s) > 512 {
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
			if err := replicaJSONValue(d, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "peers", "referenceUIDs", "omittedReferences", "excludedReferences":
			limit = MaxPeers
		case "comparisons":
			limit = len(policies)
		case "caveats":
			limit = 8
		}
		for n := 0; d.More(); n++ {
			if n >= limit {
				return ErrBounds
			}
			if err := replicaJSONValue(d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	if err != nil {
		return ErrInvalid
	}
	return nil
}
