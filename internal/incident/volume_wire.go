package incident

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func decodeVolume(data []byte) (VolumeBundle, error) {
	if len(data) > MaxVolumeBytes {
		return VolumeBundle{}, fmt.Errorf("volume incident exceeds file byte limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := volumeWireValue(d, "", 0, new(int)); err != nil {
		return VolumeBundle{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		return VolumeBundle{}, fmt.Errorf("unexpected trailing volume incident JSON")
	}
	var b VolumeBundle
	if err := decodeStrict(data, &b); err != nil {
		return VolumeBundle{}, fmt.Errorf("invalid volume incident fields")
	}
	return b, ValidateVolume(b)
}

func volumeWireValue(d *json.Decoder, field string, depth int, tokens *int) error {
	invalid := fmt.Errorf("invalid bounded volume incident JSON")
	*tokens++
	if depth > 24 || *tokens > 200000 {
		return invalid
	}
	token, err := d.Token()
	if err != nil {
		return invalid
	}
	if depth == 1 && strings.EqualFold(field, "redacted") {
		if _, ok := token.(bool); !ok {
			return invalid
		}
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if value, ok := token.(string); ok && len(value) > 4096 {
			return invalid
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 253 || len(seen) >= 256 || seen[strings.ToLower(key)] {
				return invalid
			}
			seen[strings.ToLower(key)] = true
			if err := volumeWireValue(d, key, depth+1, tokens); err != nil {
				return err
			}
		}
		if depth == 0 {
			for _, required := range []string{"schemaversion", "capturedat", "toolversion", "redacted", "pod", "volumes"} {
				if !seen[required] {
					return invalid
				}
			}
		}
	case '[':
		limit := 0
		switch field {
		case "containers":
			limit = 256
		case "volumes":
			limit = 64
		case "health":
			limit = 3
		case "conditions", "caveats":
			limit = 16
		case "points":
			limit = MaxVolumeHistoryPoints
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return invalid
			}
			if err := volumeWireValue(d, "", depth+1, tokens); err != nil {
				return err
			}
		}
	default:
		return invalid
	}
	_, err = d.Token()
	return err
}
