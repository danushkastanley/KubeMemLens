package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Collector contracts allow objects and scalar values only.
func unambiguousJSON(data []byte) error {
	return boundedJSON(data, 0)
}

// Scan diagnostics additionally carry one bounded history array. Typed decoding
// still rejects arrays at any other field or level of the schema.
func boundedJSON(data []byte, arrayLimit int) error {
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
		if delim == '[' {
			return visitArray(d, depth, arrayLimit, visit)
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

func visitArray(d *json.Decoder, depth, maximum int, visit func(int) error) error {
	if maximum == 0 {
		return errObservation
	}
	count := 0
	for d.More() {
		if count >= maximum || visit(depth+1) != nil {
			return errObservation
		}
		count++
	}
	token, err := d.Token()
	if err != nil || token != json.Delim(']') {
		return errObservation
	}
	return nil
}
