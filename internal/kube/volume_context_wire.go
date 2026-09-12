package kube

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

// The collector may read hostile namespace-owned Pod/PVC objects. Bound their
// arrays before decoding Kubernetes structs with large per-element footprints.
func boundedVolumeObject(body []byte) error {
	if len(body) > maxHealthResponse {
		return invalidHealth()
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if err := volumeObjectValue(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return invalidHealth()
	}
	return nil
}

func volumeObjectValue(d *json.Decoder, field string, depth int) error {
	if depth > 32 {
		return invalidHealth()
	}
	token, err := d.Token()
	if err != nil {
		return invalidHealth()
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 1024 || len(seen) >= 1024 {
				return invalidHealth()
			}
			key = strings.ToLower(key)
			if seen[key] {
				return invalidHealth()
			}
			seen[key] = true
			if err := volumeObjectValue(d, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 1024
		switch field {
		case "volumes", "volumemounts", "volumehealth", "ownerreferences":
			limit = volumecontext.MaxVolumesPerPod
		case "containers", "initcontainers", "ephemeralcontainers", "containerstatuses", "initcontainerstatuses", "ephemeralcontainerstatuses":
			limit = 256
		case "healthconditions":
			limit = 16
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return invalidHealth()
			}
			if err := volumeObjectValue(d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return invalidHealth()
	}
	_, err = d.Token()
	return err
}
