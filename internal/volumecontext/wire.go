package volumecontext

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

func boundedWire(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := wireValue(decoder, "", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func wireValue(decoder *json.Decoder, field string, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if text, ok := token.(string); ok && !validText(text, MaxNameBytes) {
			return ErrInvalid
		}
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 32 || len(seen) >= 16 || !wireKey(key) {
				return ErrInvalid
			}
			folded := strings.ToLower(key)
			if seen[folded] {
				return ErrInvalid
			}
			seen[folded] = true
			if err := wireValue(decoder, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		if field != "records" || depth != 1 {
			return ErrInvalid
		}
		for count := 0; decoder.More(); count++ {
			if count >= MaxBatchRecords {
				return ErrInvalid
			}
			if err := wireValue(decoder, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = decoder.Token()
	return err
}

func wireKey(key string) bool {
	switch key {
	case "schemaVersion", "nodeName", "nodeUID", "reportedAt", "state", "records",
		"namespace", "podUID", "volumeName", "pvcNamespace", "pvcName", "filesystem",
		"source", "availability", "reason", "freshness", "completeness", "capturedAt", "capacityBytes",
		"usedBytes", "availableBytes", "inodes", "inodesUsed", "inodesFree":
		return true
	default:
		return false
	}
}
