package memorytopology

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

func (r *Report) UnmarshalJSON(data []byte) error {
	type wire Report
	var result wire
	if err := decode(data, MaxReportBytes, &result); err != nil {
		return err
	}
	*r = Report(result)
	return nil
}
func (o *Observation) UnmarshalJSON(data []byte) error {
	type wire Observation
	var result wire
	if err := decode(data, MaxObservationBytes, &result); err != nil {
		return err
	}
	*o = Observation(result)
	return nil
}
func decode(data []byte, limit int, out any) error {
	if len(data) > limit || !utf8.Valid(data) {
		return ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := topologyJSONValue(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrInvalid
	}
	return nil
}

func topologyJSONValue(d *json.Decoder, field string, depth int) error {
	if depth > 12 {
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
			if err := topologyJSONValue(d, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "items", "pools":
			limit = MaxPageSizes
		case "nodes", "numa":
			limit = MaxNodes
		case "caveats":
			limit = 8
		}
		for n := 0; d.More(); n++ {
			if n >= limit {
				return ErrBounds
			}
			if err := topologyJSONValue(d, "", depth+1); err != nil {
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
