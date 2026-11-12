package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Typed snapshot fields nest about ten levels; anything deeper is not a snapshot.
const maxSnapshotWireDepth = 64

var (
	ErrSnapshotWire           = errors.New("invalid snapshot JSON")
	ErrSnapshotContainerLimit = errors.New("containers exceeds maximum")

	containersKey = []byte("containers")
)

type wireFrame struct {
	array       bool
	bounded     bool
	expectValue bool
	count       int
}

// BoundSnapshotWire checks the container count on the wire form. A few bytes
// per array element decode to a large typed cgroup record, so the limit must
// hold before any typed decode. encoding/json matches field names
// case-insensitively and decodes every duplicate, so each array under any
// spelling of "containers" is bounded at any depth.
//
// This is a single pass over the bytes because it runs on every ingest. It
// only has to count correctly for valid JSON: the typed decoder validates
// syntax before it allocates, and still classifies malformed documents.
func BoundSnapshotWire(data []byte, maxContainers int) error {
	var frames [maxSnapshotWireDepth]wireFrame
	depth := 0
	var lastString []byte
	containersNext := false

	for index := 0; index < len(data); index++ {
		char := data[index]
		switch char {
		case ' ', '\t', '\r', '\n':
			continue
		case ':':
			containersNext = depth > 0 && !frames[depth-1].array && isContainersKey(lastString)
			continue
		case ',':
			if depth > 0 {
				frames[depth-1].expectValue = true
			}
		case '}', ']':
			if depth == 0 {
				return ErrSnapshotWire
			}
			depth--
		default:
			if depth > 0 && frames[depth-1].array && frames[depth-1].expectValue {
				frame := &frames[depth-1]
				frame.expectValue = false
				frame.count++
				if frame.bounded && frame.count > maxContainers {
					return fmt.Errorf("%w of %d", ErrSnapshotContainerLimit, maxContainers)
				}
			}
			switch char {
			case '"':
				end := stringEnd(data, index)
				if end < 0 {
					return ErrSnapshotWire
				}
				lastString = data[index+1 : end]
				index = end
			case '{', '[':
				if depth == maxSnapshotWireDepth {
					return ErrSnapshotWire
				}
				frames[depth] = wireFrame{array: char == '[', bounded: char == '[' && containersNext, expectValue: true}
				depth++
			default:
				index = scalarEnd(data, index)
			}
		}
		containersNext = false
	}
	if depth != 0 {
		return ErrSnapshotWire
	}
	return nil
}

// stringEnd returns the index of the closing quote of the string that opens
// at start, or -1 when the string is not terminated.
func stringEnd(data []byte, start int) int {
	for index := start + 1; index < len(data); index++ {
		switch data[index] {
		case '\\':
			index++
		case '"':
			return index
		}
	}
	return -1
}

// scalarEnd returns the index of the last byte of the number or literal that
// begins at start.
func scalarEnd(data []byte, start int) int {
	for index := start + 1; index < len(data); index++ {
		switch data[index] {
		case ' ', '\t', '\r', '\n', ',', ':', '"', '{', '}', '[', ']':
			return index - 1
		}
	}
	return len(data) - 1
}

func isContainersKey(raw []byte) bool {
	// Neither case folding nor escaping makes a spelling shorter than the name.
	if len(raw) < len(containersKey) {
		return false
	}
	if bytes.IndexByte(raw, '\\') < 0 {
		return bytes.EqualFold(raw, containersKey)
	}
	// An escaped spelling such as "cont\u0061iners" names the same field.
	quoted := make([]byte, 0, len(raw)+2)
	quoted = append(append(append(quoted, '"'), raw...), '"')
	var key string
	return json.Unmarshal(quoted, &key) == nil && bytes.EqualFold([]byte(key), containersKey)
}
