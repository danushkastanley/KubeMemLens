package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Reject duplicate or case-aliased selectors before decoding the private input.
func uniqueConfigurationKeys(raw []byte) bool {
	allowed := strings.Fields("server token caPEM networkScope workerBootID sessionID engineDigest programmeDigest target durationSeconds maxEvents maxOutputBytes maxMapBytes maxPathBytes")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return false
		}
		valid := false
		for _, name := range allowed {
			if key == name {
				valid = true
				break
			}
		}
		if !valid {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}
