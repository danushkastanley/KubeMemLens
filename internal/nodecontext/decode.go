package nodecontext

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

var ErrInvalidObservation = errors.New("invalid bounded Node observation")

// UnmarshalJSON limits the normalised record before allocating typed slices.
// Unlike the upstream Summary parser, this private wire contract is strict.
func (o *Observation) UnmarshalJSON(data []byte) error {
	if len(data) > MaxObservationBytes {
		return ErrInvalidObservation
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := checkJSONValue(d, "", 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidObservation
	}
	type wire Observation
	var value wire
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return ErrInvalidObservation
	}
	*o = Observation(value)
	return nil
}

func checkJSONValue(d *json.Decoder, field string, depth int) error {
	if depth > 8 {
		return ErrInvalidObservation
	}
	token, err := d.Token()
	if err != nil {
		return ErrInvalidObservation
	}
	delim, container := token.(json.Delim)
	if !container {
		switch field {
		case "some", "full", "totalNanoseconds", "avg10", "avg60", "avg300":
			if token == nil {
				return ErrInvalidObservation
			}
		}
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for d.More() {
			token, err := d.Token()
			key, ok := token.(string)
			if err != nil || !ok || !observationKey(key) || len(seen) >= 32 {
				return ErrInvalidObservation
			}
			if _, exists := seen[key]; exists {
				return ErrInvalidObservation
			}
			seen[key] = struct{}{}
			if err := checkJSONValue(d, key, depth+1); err != nil {
				return err
			}
		}
		var required []string
		switch field {
		case "psi":
			required = []string{"some", "full"}
		case "some", "full":
			required = []string{"totalNanoseconds", "avg10", "avg60", "avg300"}
		}
		for _, key := range required {
			if _, ok := seen[key]; !ok {
				return ErrInvalidObservation
			}
		}

	case '[':
		limit := 0
		switch field {
		case "systemContainers":
			limit = MaxSystemContainers
		case "hugepages":
			limit = MaxHugepages
		case "caveats":
			limit = MaxCaveats
		}
		for count := 0; d.More(); count++ {
			if count >= limit {
				return ErrInvalidObservation
			}
			if err := checkJSONValue(d, "", depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalidObservation
	}
	_, err = d.Token()
	return err
}

// encoding/json otherwise accepts case-insensitive aliases for struct fields.
func observationKey(key string) bool {
	switch key {
	case "nodeName", "nodeUID", "reportedAt", "availability", "reason", "evidence", "stats", "context",
		"source", "apiVersion", "capturedAt", "receivedAt", "windowNanoseconds", "scope", "freshness", "completeness", "capability", "caveats",
		"startedAt", "provenance", "memory", "swap", "systemContainers", "availableBytes", "usageBytes", "workingSetBytes", "rssBytes",
		"pageFaults", "majorPageFaults", "psi", "some", "full", "totalNanoseconds", "avg10", "avg60", "avg300", "category",
		"capacityBytes", "allocatableBytes", "memoryPressure", "hugepages", "resource":
		return true
	default:
		return false
	}
}
