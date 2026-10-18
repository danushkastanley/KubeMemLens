package incident

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func decodeHistory(data []byte) (HistoryBundle, error) {
	invalid := fmt.Errorf("invalid bounded memory history incident")
	if len(data) > MaxHistoryBytes || !utf8.Valid(data) {
		return HistoryBundle{}, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return HistoryBundle{}, invalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || len(key) > 32 || seen[strings.ToLower(key)] {
			return HistoryBundle{}, invalid
		}
		switch key {
		case "schemaVersion", "capturedAt", "toolVersion", "redacted", "context", "caveats":
		default:
			return HistoryBundle{}, invalid
		}
		seen[strings.ToLower(key)] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return HistoryBundle{}, invalid
		}
		if key == "redacted" && string(raw) != "true" && string(raw) != "false" {
			return HistoryBundle{}, invalid
		}
	}
	if len(seen) != 6 {
		return HistoryBundle{}, invalid
	}
	if _, err := decoder.Token(); err != nil {
		return HistoryBundle{}, invalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return HistoryBundle{}, invalid
	}
	var b HistoryBundle
	// The capture has its own formatted-file bound. Nested API evidence uses
	// compact byte budgets; indentation added by our writer is not evidence.
	// Compact preserves duplicate keys and string contents for strict decoding.
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return HistoryBundle{}, invalid
	}
	if err := decodeStrict(compact.Bytes(), &b); err != nil {
		return HistoryBundle{}, invalid
	}
	return b, ValidateHistory(b)
}
