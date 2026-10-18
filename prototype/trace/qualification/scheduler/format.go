package scheduler

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

var fieldLine = regexp.MustCompile(`^field:(.+);\s*offset:([0-9]+);\s*size:([0-9]+);\s*signed:([01]);$`)
var fieldName = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)(?:\[[0-9]+\])?$`)

// ParseFormat checks the kernel's bounded tracefs schema, not payload text.
// Field layouts are retained for numeric projection only. The controller must
// freeze the complete format hash with the exact kernel/profile before use.
func ParseFormat(text string) (Format, error) {
	f := Format{fields: make(map[string]field)}
	if len(text) == 0 || len(text) > 16384 {
		return f, ErrObservation
	}
	var name string
	seenID := false
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
			f.id = uint16(id)
			seenID = true
			continue
		}
		if !strings.HasPrefix(line, "field:") {
			continue
		}
		parts := fieldLine.FindStringSubmatch(line)
		if len(parts) != 5 || len(f.fields) >= 32 {
			return f, ErrObservation
		}
		match := fieldName.FindStringSubmatch(parts[1])
		if len(match) != 2 {
			return f, ErrObservation
		}
		key := match[1]
		if _, exists := f.fields[key]; exists {
			return f, ErrObservation
		}
		offset, e1 := strconv.Atoi(parts[2])
		size, e2 := strconv.Atoi(parts[3])
		if e1 != nil || e2 != nil || offset < 0 || size < 1 || offset > 1024-size {
			return f, ErrObservation
		}
		for _, other := range f.fields {
			if offset < other.offset+other.size && other.offset < offset+size {
				return f, ErrObservation
			}
		}
		f.fields[key] = field{offset, size, parts[4] == "1"}
		if offset+size > f.minimum {
			f.minimum = offset + size
		}
	}
	if !seenID {
		return f, ErrObservation
	}
	switch name {
	case "sched_wakeup":
		f.kind = Wake
	case "sched_wakeup_new":
		f.kind = NewTask
	case "sched_switch":
		f.kind = Switch
	case "sched_process_exit":
		f.kind = Exit
	default:
		return f, ErrObservation
	}
	typeField, ok := f.fields["common_type"]
	if !ok || typeField.offset != 0 || typeField.size != 2 || typeField.signed {
		return f, ErrObservation
	}
	names := []string{"pid"}
	if f.kind == Switch {
		names = []string{"prev_pid", "next_pid", "prev_state"}
	}
	for _, key := range names {
		value, ok := f.fields[key]
		size := 4
		if key == "prev_state" {
			size = 8
		}
		if !ok || value.size != size || !value.signed {
			return f, ErrObservation
		}
	}
	return f, nil
}

func (f Format) pid(raw []byte, name string) (uint32, error) {
	value := f.fields[name]
	pid := binary.LittleEndian.Uint32(raw[value.offset : value.offset+4])
	if pid > 0x7fffffff {
		return 0, ErrObservation
	}
	return pid, nil
}

// Decode supports the frozen Linux amd64/arm64 little-endian tracepoint ABI.
// The documented sched_switch state encoding is TASK_RUNNING=0, preemption=256
// and single-bit sleeping/exiting states up to128. Unknown encodings fail.
// Raw comm/name bytes are never read into strings or returned to the controller.
func (f Format) Decode(raw []byte, stamp uint64) (Event, error) {
	if len(raw) < f.minimum || len(raw) > 1024 || len(raw) < 2 || f.id == 0 || binary.LittleEndian.Uint16(raw[:2]) != f.id {
		return Event{}, ErrObservation
	}
	e := Event{Kind: f.kind, Time: stamp}
	var err error
	if f.kind != Switch {
		e.PID, err = f.pid(raw, "pid")
	} else {
		e.PrevPID, err = f.pid(raw, "prev_pid")
		if err != nil {
			return Event{}, err
		}
		e.NextPID, err = f.pid(raw, "next_pid")
		if err != nil {
			return Event{}, err
		}
		start := f.fields["prev_state"].offset
		state := binary.LittleEndian.Uint64(raw[start : start+8])
		if state > 256 || (state != 0 && state&(state-1) != 0) {
			return Event{}, ErrObservation
		}
		e.PrevRunnable = state == 0 || state == 256
	}
	if err != nil || !valid(e) {
		return Event{}, ErrObservation
	}
	return e, nil
}
