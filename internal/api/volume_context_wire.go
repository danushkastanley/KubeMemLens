package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

// UnmarshalJSON bounds nested slices before constructing the typed response.
func (v *PodVolumeContext) UnmarshalJSON(data []byte) error {
	if len(data) > volumecontext.MaxPageBytes {
		return fmt.Errorf("volume response exceeds byte limit")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := volumeReadValue(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("invalid volume response JSON")
	}
	type wire PodVolumeContext
	var result wire
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&result); err != nil {
		return fmt.Errorf("invalid volume response fields")
	}
	*v = PodVolumeContext(result)
	return nil
}

func volumeReadValue(d *json.Decoder, field string, depth int) error {
	invalid := fmt.Errorf("invalid bounded volume response")
	if depth > 12 {
		return invalid
	}
	token, err := d.Token()
	if err != nil {
		return invalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if value, ok := token.(string); ok && len(value) > 1024 {
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
			if err != nil || !ok || len(key) > 64 || len(seen) >= 64 {
				return invalid
			}
			folded := strings.ToLower(key)
			if seen[folded] {
				return invalid
			}
			seen[folded] = true
			if err := volumeReadValue(d, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "volumes":
			limit = volumecontext.MaxVolumesPerPod
		case "health":
			limit = 3
		case "conditions":
			limit = 16
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return invalid
			}
			if err := volumeReadValue(d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return invalid
	}
	_, err = d.Token()
	return err
}
