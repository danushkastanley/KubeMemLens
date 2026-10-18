package traceframe

import (
	"bytes"
	"encoding/json"
	"io"
)

// Normal event frames contain only typed fields, with no RawMessage subtree.
// Exact re-encoding proves their key spelling, uniqueness, placement and required
// nullable fields without a second token walk. Semantic and stream validation
// still run. Metadata, rich summaries and every noncanonical encoding retain the
// original strict parser, including support for legal whitespace/key ordering.
func decodeFrameSyntax(data []byte) (envelope, error) {
	var e envelope
	if json.Unmarshal(data, &e) != nil {
		return envelope{}, ErrInvalid
	}
	if canonicalEvent(data, e) {
		return e, nil
	}
	return decodeStrictSyntax(data)
}

func canonicalEvent(data []byte, e envelope) bool {
	if e.Type != EventFrame || e.Event == nil || e.Metadata != nil || e.Summary != nil {
		return false
	}
	// Compare the encoder's output in place rather than allocating a second
	// encoded frame. Encode includes the required final newline.
	match := canonicalMatch{remaining: data}
	return json.NewEncoder(&match).Encode(e) == nil && len(match.remaining) == 0
}

type canonicalMatch struct{ remaining []byte }

func (m *canonicalMatch) Write(data []byte) (int, error) {
	if !bytes.HasPrefix(m.remaining, data) {
		return 0, ErrInvalid
	}
	m.remaining = m.remaining[len(data):]
	return len(data), nil
}

func decodeStrictSyntax(data []byte) (envelope, error) {
	keys := json.NewDecoder(bytes.NewReader(data))
	if uniqueValue(keys, 0, "root") != nil {
		return envelope{}, ErrInvalid
	}
	if _, err := keys.Token(); err != io.EOF {
		return envelope{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var e envelope
	if decoder.Decode(&e) != nil {
		return envelope{}, ErrInvalid
	}
	return e, nil
}
