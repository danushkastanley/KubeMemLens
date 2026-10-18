package incidentsession

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

type syntaxBudget struct {
	decoder *json.Decoder
	tokens  int
}

func boundedSyntax(data []byte) error {
	if !utf8.Valid(data) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	b := syntaxBudget{decoder: d}
	if b.value(0, "") != nil {
		return ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func (b *syntaxBudget) token() (json.Token, error) {
	b.tokens++
	if b.tokens > 8192 {
		return nil, ErrInvalid
	}
	return b.decoder.Token()
}

func (b *syntaxBudget) value(depth int, field string) error {
	if depth > 5 {
		return ErrInvalid
	}
	token, err := b.token()
	if err != nil {
		return ErrInvalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		if token == nil && field != "observedAt" {
			return ErrInvalid
		}
		return nil
	}
	switch delim {
	case '{':
		start := b.decoder.InputOffset() - 1
		seen := map[string]bool{}
		memberLimit := 16
		if field == "traceReference" {
			memberLimit = 22
		}
		for b.decoder.More() {
			token, err := b.token()
			if err != nil {
				return ErrInvalid
			}
			key, ok := token.(string)
			if !ok || len(key) > 32 || seen[key] || len(seen) >= memberLimit {
				return ErrInvalid
			}
			seen[key] = true
			if b.value(depth+1, key) != nil {
				return ErrInvalid
			}
		}
		if token, err := b.token(); err != nil || token != json.Delim('}') {
			return ErrInvalid
		}
		if field == "entries" && b.decoder.InputOffset()-start > MaxEntryBytes {
			return ErrInvalid
		}
		return requiredFields(field, seen)
	case '[':
		limit := 2
		if field == "entries" {
			limit = MaxEntries
		} else if field == "captures" {
			limit = MaxCaptures
		} else if field != "references" {
			return ErrInvalid
		}
		count := 0
		for b.decoder.More() {
			count++
			if count > limit || b.value(depth+1, field) != nil {
				return ErrInvalid
			}
		}
		if token, err := b.token(); err != nil || token != json.Delim(']') {
			return ErrInvalid
		}
		return nil
	default:
		return ErrInvalid
	}
}

func requiredFields(field string, seen map[string]bool) error {
	var required []string
	switch field {
	case "":
		required = []string{"kind", "schemaVersion", "redacted", "openedAt", "expiresAt", "entries", "limitReached"}
	case "entries":
		required = []string{"sequence", "kind", "recordedAt", "observedAt", "namespace", "actor", "source", "sensitivity", "clockUncertain"}
	case "captures":
		required = []string{"namespaceUID", "digest", "schemaVersion", "observedAt", "data"}
	case "references":
		required = []string{"schemaVersion", "observedAt"}
	case "traceReference":
		required = []string{"provenance", "reportSchemaVersion", "traceKind", "state", "cleanup", "termination", "coverage", "transportComplete"}
	case "produced", "sampled", "lost", "rejected":
		required = []string{"known", "value"}
	default:
		return ErrInvalid
	}
	for _, name := range required {
		if !seen[name] {
			return ErrInvalid
		}
	}
	return nil
}
