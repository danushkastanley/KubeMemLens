package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// These contracts contain objects and scalar values only. Reject aliases,
// duplicate keys and excess nesting before allocating the typed observation.
func unambiguousJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 4 {
			return errObservation
		}
		token, err := d.Token()
		if err != nil {
			return errObservation
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delim != '{' {
			return errObservation
		}
		seen := map[string]bool{}
		for d.More() {
			token, err = d.Token()
			key, ok := token.(string)
			if err != nil || !ok || len(key) > 64 || len(seen) >= 32 {
				return errObservation
			}
			key = strings.ToLower(key)
			if seen[key] {
				return errObservation
			}
			seen[key] = true
			if visit(depth+1) != nil {
				return errObservation
			}
		}
		token, err = d.Token()
		if err != nil || token != json.Delim('}') {
			return errObservation
		}
		return nil
	}
	if visit(0) != nil {
		return errObservation
	}
	if _, err := d.Token(); err != io.EOF {
		return errObservation
	}
	return nil
}
