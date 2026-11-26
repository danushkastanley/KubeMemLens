package scheduler

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func formatFixture(name string) string {
	text := "name: " + name + "\nID: 204\nformat:\nfield:unsigned short common_type; offset:0; size:2; signed:0;\nfield:char comm[16]; offset:8; size:16; signed:0;\n"
	if name == "sched_switch" {
		return text + "field:pid_t prev_pid; offset:24; size:4; signed:1;\nfield:long prev_state; offset:32; size:8; signed:1;\nfield:pid_t next_pid; offset:56; size:4; signed:1;\n"
	}
	return text + "field:pid_t pid; offset:24; size:4; signed:1;\n"
}

func TestTracepointProjectionIgnoresTaskNamesAndUsesValidatedOffsets(t *testing.T) {
	for _, name := range []string{"sched_wakeup", "sched_wakeup_new", "sched_process_exit", "sched_switch"} {
		f, err := ParseFormat(formatFixture(name))
		if err != nil {
			t.Fatal(err)
		}
		raw := make([]byte, 64)
		binary.LittleEndian.PutUint16(raw, 204)
		copy(raw[8:24], "private-taskname!")
		binary.LittleEndian.PutUint32(raw[24:], 7)
		if name == "sched_switch" {
			binary.LittleEndian.PutUint32(raw[56:], 8)
			binary.LittleEndian.PutUint64(raw[32:], 256)
		}
		e, err := f.Decode(raw, 100)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(e), "private-taskname") {
			t.Fatal("task name escaped")
		}
		if name == "sched_switch" && (!e.PrevRunnable || e.PrevPID != 7 || e.NextPID != 8) {
			t.Fatal(e)
		}
		if name != "sched_switch" && e.PID != 7 {
			t.Fatal(e)
		}
		for _, bad := range [][]byte{raw[:16], append(raw, make([]byte, 1025)...)} {
			if _, err := f.Decode(bad, 100); err == nil {
				t.Fatal("unbounded sample")
			}
		}
		raw[0] = 1
		if _, err := f.Decode(raw, 100); err == nil {
			t.Fatal("wrong tracepoint identity")
		}
	}
}

func TestUnsupportedOrAmbiguousTracepointLayoutsFail(t *testing.T) {
	good := formatFixture("sched_switch")
	for _, bad := range []string{strings.Replace(good, "size:8; signed:1", "size:4; signed:1", 1), strings.Replace(good, "offset:56", "offset:24", 1), strings.Replace(good, "prev_pid", "other_pid", 1), good + "ID: 205\n", good + "field:pid_t prev_pid; offset:80; size:4; signed:1;\n", strings.Replace(good, "ID: 204", "ID: 65536", 1), strings.Replace(good, "sched_switch", "unapproved", 1), strings.Repeat(" ", 16385)} {
		if _, err := ParseFormat(bad); err == nil {
			t.Fatal("invalid kernel schema accepted")
		}
	}
}

func TestSwitchStateNeverTurnsSleepingIntoRunnable(t *testing.T) {
	f, _ := ParseFormat(formatFixture("sched_switch"))
	raw := make([]byte, 64)
	binary.LittleEndian.PutUint16(raw, 204)
	binary.LittleEndian.PutUint32(raw[24:], 7)
	binary.LittleEndian.PutUint32(raw[56:], 8)
	for _, state := range []uint64{0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 3, 512, ^uint64(0)} {
		binary.LittleEndian.PutUint64(raw[32:], state)
		e, err := f.Decode(raw, 100)
		supported := state <= 256 && (state == 0 || state&(state-1) == 0)
		if (err == nil) != supported {
			t.Fatal(state, e, err)
		}
		if supported && e.PrevRunnable != (state == 0 || state == 256) {
			t.Fatal("sleeping task marked runnable")
		}
	}
}

//go:embed testdata/linuxkit-7.0.12-arm64.json
var retainedFormats []byte

func TestRetainedLinuxKernelFormatsDecodeWithoutTaskNames(t *testing.T) {
	raw := retainedFormats
	var formats map[string]string
	if json.Unmarshal(raw, &formats) != nil || len(formats) != 4 {
		t.Fatal("invalid retained schemas")
	}
	for name, text := range formats {
		f, err := ParseFormat(text)
		if err != nil {
			t.Fatal(name, err)
		}
		data := make([]byte, f.minimum)
		binary.LittleEndian.PutUint16(data, f.id)
		for _, key := range []string{"pid", "prev_pid", "next_pid"} {
			if value, exists := f.fields[key]; exists {
				pid := uint32(7)
				if key == "next_pid" {
					pid = 8
				}
				binary.LittleEndian.PutUint32(data[value.offset:], pid)
			}
		}
		if _, err := f.Decode(data, 100); err != nil {
			t.Fatal(name, err)
		}
	}
}
