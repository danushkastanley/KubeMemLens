package verifier

import (
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
)

type field struct {
	offset, size int
	signed       bool
}
type Format struct {
	kind    Kind
	id      uint16
	fields  map[string]field
	minimum int
}

var fieldLine = regexp.MustCompile(`^field:.*\b([A-Za-z_][A-Za-z0-9_]*);\s*offset:([0-9]+);\s*size:([0-9]+);\s*signed:([01]);$`)

// ParseFormat accepts only the complete numeric schema of one fixed owned probe.
// Native callers must additionally freeze the raw format hash and kernel ABI.
func (p ProbePlan) ParseFormat(text string, kind Kind) (Format, error) {
	f := Format{kind: kind, fields: make(map[string]field)}
	definitions, err := p.Definitions()
	if err != nil || kind < CheckEnter || kind > CheckReturn || len(text) == 0 || len(text) > 16384 {
		return f, ErrObservation
	}
	index := map[Kind]int{CheckEnter: 0, CheckReturn: 1, LogFinalized: 2}[kind]
	_, expectedName, _ := strings.Cut(definitions[index].Name, "/")
	name, seenID := "", false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name: ") {
			if name != "" {
				return f, ErrObservation
			}
			name = strings.TrimPrefix(line, "name: ")
			continue
		}
		if strings.HasPrefix(line, "ID: ") {
			id, err := strconv.ParseUint(strings.TrimPrefix(line, "ID: "), 10, 16)
			if err != nil || id == 0 || seenID {
				return f, ErrObservation
			}
			f.id, seenID = uint16(id), true
			continue
		}
		if !strings.HasPrefix(line, "field:") {
			continue
		}
		parts := fieldLine.FindStringSubmatch(line)
		if len(parts) != 5 || len(f.fields) >= 8 {
			return f, ErrObservation
		}
		if _, exists := f.fields[parts[1]]; exists {
			return f, ErrObservation
		}
		offset, e1 := strconv.Atoi(parts[2])
		size, e2 := strconv.Atoi(parts[3])
		if e1 != nil || e2 != nil || size < 1 || size > 8 || offset < 0 || offset > 256-size {
			return f, ErrObservation
		}
		for _, other := range f.fields {
			if offset < other.offset+other.size && other.offset < offset+size {
				return f, ErrObservation
			}
		}
		f.fields[parts[1]] = field{offset, size, parts[4] == "1"}
		f.minimum = max(f.minimum, offset+size)
	}
	if !seenID || name != expectedName {
		return f, ErrObservation
	}
	expected := map[string]field{"common_type": {0, 2, false}, "common_flags": {2, 1, false},
		"common_preempt_count": {3, 1, false}, "common_pid": {4, 4, true}}
	if kind == CheckEnter {
		expected["__probe_ip"] = field{8, 8, false}
	} else {
		expected["__probe_func"] = field{8, 8, false}
		expected["__probe_ret_ip"] = field{16, 8, false}
		expected["result"] = field{24, 4, true}
		if kind == LogFinalized {
			expected["bytes"] = field{28, 4, false}
		}
	}
	if len(f.fields) != len(expected) {
		return f, ErrObservation
	}
	for name, value := range expected {
		if f.fields[name] != value {
			return f, ErrObservation
		}
	}
	return f, nil
}

// Decode never reads the automatic probe address fields. This schema is limited
// to the verified 64-bit little-endian kernel layout, not arbitrary trace data.
func (f Format) Decode(raw []byte, stamp uint64, tid uint32) (Event, error) {
	if f.id == 0 || len(raw) < f.minimum || len(raw) > 256 || len(raw) < 8 || stamp == 0 ||
		tid == 0 || tid > 0x7fffffff || binary.LittleEndian.Uint16(raw[:2]) != f.id ||
		binary.LittleEndian.Uint32(raw[4:8]) != tid {
		return Event{}, ErrObservation
	}
	event := Event{Kind: f.kind, Time: stamp, TID: tid}
	if f.kind != CheckEnter {
		event.Result = int32(binary.LittleEndian.Uint32(raw[24:28]))
		if event.Result > 0 || event.Result < -4095 {
			return Event{}, ErrObservation
		}
	}
	if f.kind == LogFinalized {
		event.LogSizeBytes = binary.LittleEndian.Uint32(raw[28:32])
	}
	return event, nil
}
