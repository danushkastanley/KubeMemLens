package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// Check the Node-only envelope before decoding the shared AgentSnapshot type.
// Otherwise a small array of empty container objects can allocate large cgroup
// structs before role validation rejects them. Duplicate aliases must also fail
// here so an empty final field cannot conceal an earlier allocating field.
func boundedNodeWire(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := nodeWireValue(decoder, "", 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nodecontext.ErrInvalidObservation
	}
	return nil
}

func nodeWireValue(decoder *json.Decoder, field string, depth int) error {
	if depth > 16 {
		return nodecontext.ErrInvalidObservation
	}
	token, err := decoder.Token()
	if err != nil {
		return nodecontext.ErrInvalidObservation
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 64 || len(seen) >= 64 {
				return nodecontext.ErrInvalidObservation
			}
			key = strings.ToLower(key)
			if seen[key] {
				return nodecontext.ErrInvalidObservation
			}
			seen[key] = true
			if err := nodeWireValue(decoder, key, depth+1); err != nil {
				return err
			}
		}
	case '[':
		limit := 0
		switch field {
		case "systemcontainers":
			limit = nodecontext.MaxSystemContainers
		case "hugepages":
			limit = nodecontext.MaxHugepages
		case "caveats":
			limit = nodecontext.MaxCaveats
		}
		for count := 0; decoder.More(); count++ {
			if count >= limit {
				return nodecontext.ErrInvalidObservation
			}
			if err := nodeWireValue(decoder, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return nodecontext.ErrInvalidObservation
	}
	_, err = decoder.Token()
	return err
}
