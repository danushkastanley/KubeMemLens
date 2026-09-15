package filecache

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func oomRecord() []byte {
	data := make([]byte, 40)
	binary.LittleEndian.PutUint64(data, 200)
	binary.LittleEndian.PutUint32(data[8:], 1)
	binary.LittleEndian.PutUint32(data[12:], 1234)
	binary.LittleEndian.PutUint32(data[16:], 3)
	copy(data[24:], "fixture")
	return data
}

func TestOOMScopeTimeAndMissingContext(t *testing.T) {
	d := decoder(t, trace.OOM, trace.OmitPaths)
	for value, scope := range []trace.OOMScope{trace.OOMScopeUnknown, trace.OOMScopeCgroup, trace.OOMScopeGlobal} {
		data := oomRecord()
		binary.LittleEndian.PutUint32(data[8:], uint32(value))
		event, err := d.OOM(data)
		if err != nil || event.Scope != scope || event.VictimPID == nil || *event.VictimPID != 1234 || !event.ObservedAt.Equal(time.Unix(20, 100)) {
			t.Fatal("OOM scope, victim or time lost")
		}
		if _, err := traceframe.NewOOM(event, d.spec); err != nil {
			t.Fatal("valid OOM observation cannot enter ephemeral stream")
		}
	}
	data := oomRecord()
	clear(data[12:40])
	event, err := d.OOM(data)
	if err != nil || event.VictimPID != nil || event.Command.Reveal() != "" {
		t.Fatal("missing context became a process identity")
	}
	data = oomRecord()
	data[24] = 255
	event, err = d.OOM(data)
	if err != nil || event.VictimPID == nil || event.Command.Reveal() != "" {
		t.Fatal("non-UTF-8 task command was substituted or retained")
	}
}

func TestOOMRejectsMalformedRecordsAndWrongKind(t *testing.T) {
	d := decoder(t, trace.OOM, trace.OmitPaths)
	mutations := map[string]func([]byte){
		"unknown scope":    func(b []byte) { b[8] = 3 },
		"unknown flags":    func(b []byte) { b[16] = 4 },
		"missing pid flag": func(b []byte) { b[16] = 2 },
		"zero present pid": func(b []byte) { clear(b[12:16]) },
		"negative pid":     func(b []byte) { b[15] = 255 },
		"hidden command":   func(b []byte) { b[16] = 1 },
		"padding":          func(b []byte) { b[20] = 1 },
		"command suffix":   func(b []byte) { b[39] = 1 },
		"before window":    func(b []byte) { binary.LittleEndian.PutUint64(b, 99) },
		"after window":     func(b []byte) { binary.LittleEndian.PutUint64(b, 200+uint64(time.Second)) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			data := oomRecord()
			mutate(data)
			if _, err := d.OOM(data); err == nil {
				t.Fatal("malformed OOM record accepted")
			}
		})
	}
	if _, err := d.OOM(oomRecord()[:39]); err == nil {
		t.Fatal("truncated record accepted")
	}
	if _, err := decoder(t, trace.Files, trace.OmitPaths).OOM(oomRecord()); err == nil {
		t.Fatal("OOM record crossed trace-kind boundary")
	}
}

func TestCompiledOOMCandidates(t *testing.T) {
	root := os.Getenv("KML_OOM_OBJECTS")
	if root == "" {
		t.Skip("requires offline build_filecache.py --set all output")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		data, err := os.ReadFile(filepath.Join(root, "oom-"+arch+"-1", "program.bpf.o"))
		if err != nil {
			t.Fatal(err)
		}
		inventory, err := Inspect(trace.OOM, data)
		if err != nil || inventory.EventSize != 40 || len(inventory.Hooks) != 5 || len(inventory.Maps) != 8 {
			t.Fatal("OOM object inventory differs")
		}
		if err := ValidateObject(trace.OOM, data); err != nil {
			t.Fatal("OOM object violates fixed policy")
		}
	}
}
