package verifier

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// Synthetic ABI fixtures exercise projection only; native kernel layout and
// saved-argument semantics still require separate validation before capture.
func formatFixture(kind Kind) (ProbePlan, string) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	definitions, _ := plan.Definitions()
	index := map[Kind]int{CheckEnter: 0, CheckReturn: 1, LogFinalized: 2}[kind]
	_, name, _ := strings.Cut(definitions[index].Name, "/")
	text := "name: " + name + "\nID: 123\nformat:\n"
	fields := []struct {
		name                 string
		offset, size, signed int
	}{
		{"common_type", 0, 2, 0}, {"common_flags", 2, 1, 0},
		{"common_preempt_count", 3, 1, 0}, {"common_pid", 4, 4, 1},
	}
	if kind == CheckEnter {
		fields = append(fields, struct {
			name                 string
			offset, size, signed int
		}{"__probe_ip", 8, 8, 0})
	} else {
		fields = append(fields, struct {
			name                 string
			offset, size, signed int
		}{"__probe_func", 8, 8, 0},
			struct {
				name                 string
				offset, size, signed int
			}{"__probe_ret_ip", 16, 8, 0},
			struct {
				name                 string
				offset, size, signed int
			}{"result", 24, 4, 1})
		if kind == LogFinalized {
			fields = append(fields, struct {
				name                 string
				offset, size, signed int
			}{"bytes", 28, 4, 0})
		}
	}
	for _, f := range fields {
		text += fmt.Sprintf(" field:unsigned long %s; offset:%d; size:%d; signed:%d;\n", f.name, f.offset, f.size, f.signed)
	}
	return plan, text
}

func sampleFixture(kind Kind) (Format, []byte) {
	plan, text := formatFixture(kind)
	f, err := plan.ParseFormat(text, kind)
	if err != nil {
		panic(err)
	}
	raw := make([]byte, f.minimum)
	binary.LittleEndian.PutUint16(raw, f.id)
	binary.LittleEndian.PutUint32(raw[4:8], 42)
	// Deliberately non-zero addresses; the projection must ignore them.
	for i := 8; i < min(len(raw), 24); i++ {
		raw[i] = 0xed
	}
	if kind != CheckEnter {
		binary.LittleEndian.PutUint32(raw[24:28], 0)
	}
	if kind == LogFinalized {
		binary.LittleEndian.PutUint32(raw[28:32], 129)
	}
	data := make([]byte, (44+len(raw)+7)&^7)
	binary.LittleEndian.PutUint32(data, 9)
	binary.LittleEndian.PutUint16(data[6:8], uint16(len(data)))
	binary.LittleEndian.PutUint64(data[8:16], 5)
	binary.LittleEndian.PutUint32(data[16:20], 40)
	binary.LittleEndian.PutUint32(data[20:24], 42)
	binary.LittleEndian.PutUint64(data[24:32], 99)
	binary.LittleEndian.PutUint32(data[32:36], 3)
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(raw)))
	copy(data[44:], raw)
	return f, data
}

func TestFixedProbeFormatProjectsOnlyAllowedNumbers(t *testing.T) {
	for _, kind := range []Kind{CheckEnter, LogFinalized, CheckReturn} {
		f, data := sampleFixture(kind)
		event, err := Record(data, 3, map[uint64]Format{5: f})
		want := Event{Kind: kind, Time: 99, TID: 42}
		if kind == LogFinalized {
			want.LogSizeBytes = 129
		}
		if err != nil || event != want {
			t.Fatal("numeric event projection changed")
		}
	}
}

func TestChangedFormatCannotBroadenOrMisreadTheCapture(t *testing.T) {
	plan, text := formatFixture(LogFinalized)
	for _, changed := range []string{
		strings.Replace(text, "ID: 123", "ID: 0", 1), text + "ID: 123\n",
		strings.Replace(text, "offset:28", "offset:24", 1),
		strings.Replace(text, "bytes;", "payload;", 1),
		strings.Replace(text, "offset:28; size:4; signed:0", "offset:28; size:4; signed:1", 1),
		strings.Replace(text, "offset:28; size:4", "offset:28; size:8", 1),
		text + "field:char extra; offset:32; size:1; signed:0;\n",
		strings.Replace(text, "common_pid", "common_pid[4]", 1),
		strings.Replace(text, "name: log_", "name: other_", 1),
	} {
		if _, err := plan.ParseFormat(changed, LogFinalized); err == nil {
			t.Fatal("changed schema accepted")
		}
	}
}

func TestRecordRejectsLossTruncationAndInvalidIdentityBounds(t *testing.T) {
	f, valid := sampleFixture(LogFinalized)
	for _, change := range []func([]byte){
		func(b []byte) { b[0] = 2 }, // loss
		func(b []byte) { b[0] = 5 }, // throttle
		func(b []byte) { b[6]-- },
		func(b []byte) { b[8]++ },
		func(b []byte) { b[16] = 0 },
		func(b []byte) { b[20] = 0 },
		func(b []byte) { b[23] = 0x80 },
		func(b []byte) { b[48] = 0 },
		func(b []byte) { b[51] = 0x80 },
		func(b []byte) { b[32]++ },
		func(b []byte) { b[36]++ },
		func(b []byte) { b[40] = 255 },
		func(b []byte) { b[44]++ },
		func(b []byte) { b[44+24] = 1 },
	} {
		data := append([]byte(nil), valid...)
		change(data)
		if _, err := Record(data, 3, map[uint64]Format{5: f}); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
	for n := 0; n < len(valid); n++ {
		if _, err := Record(valid[:n], 3, map[uint64]Format{5: f}); err == nil {
			t.Fatal("truncated record accepted")
		}
	}
}

func TestRecordMatchesInPerfCreatorsPIDNamespace(t *testing.T) {
	format, data := sampleFixture(LogFinalized)
	binary.LittleEndian.PutUint32(data[48:52], 424242)
	event, err := Record(data, 3, map[uint64]Format{5: format})
	if err != nil || event.TID != 42 {
		t.Fatal("kernel namespace ID replaced or invalidated perf matching identity")
	}
}

func FuzzRecord(f *testing.F) {
	format, data := sampleFixture(LogFinalized)
	f.Add(data)
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) { _, _ = Record(raw, 3, map[uint64]Format{5: format}) })
}
