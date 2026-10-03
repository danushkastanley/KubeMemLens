//go:build linux && kml_kernel_integration

package sdk

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/ringbuf"
)

// These opt-in tests load one unattached fixture programme and one bounded ring.
// They require an explicitly selected disposable Linux environment with BPF
// access. No probes, sockets, links, target paths or persistent pins are created.
type ringNotification int32

const (
	notifyOnSubmit  ringNotification = 0
	suppressWakeups ringNotification = 1
)

func ringFixture(t *testing.T, notification ringNotification) (*ringbuf.Reader, func()) {
	t.Helper()
	m, err := ebpf.NewMap(&ebpf.MapSpec{Name: "kml_test_ring", Type: ebpf.RingBuf, MaxEntries: 262144})
	if err != nil {
		t.Fatal(err)
	}
	var reader *ringbuf.Reader
	var programme *ebpf.Program
	var mapID ebpf.MapID
	var programmeID ebpf.ProgramID
	t.Cleanup(func() {
		if reader != nil {
			if err := reader.Close(); err != nil {
				t.Error(err)
			}
		}
		if programme != nil {
			if err := programme.Close(); err != nil {
				t.Error(err)
			}
		}
		if err := m.Close(); err != nil {
			t.Error(err)
		}
		if programmeID != 0 {
			awaitFixtureAbsence(t, func() (io.Closer, error) { return ebpf.NewProgramFromID(programmeID) })
		}
		if mapID != 0 {
			awaitFixtureAbsence(t, func() (io.Closer, error) { return ebpf.NewMapFromID(mapID) })
		}
	})
	info, err := m.Info()
	if err != nil {
		t.Fatal(err)
	}
	mapID, _ = info.ID()
	if mapID == 0 {
		t.Fatal("fixture map identity unavailable")
	}
	t.Logf("owned fixture map=%d", mapID)
	reader, err = ringbuf.NewReader(m)
	if err != nil {
		t.Fatal(err)
	}
	programme, err = ebpf.NewProgram(&ebpf.ProgramSpec{
		Name: "kml_test_emit", Type: ebpf.SocketFilter, License: "GPL",
		Instructions: asm.Instructions{
			asm.Mov.Imm(asm.R0, 42), asm.StoreMem(asm.R10, -8, asm.R0, asm.DWord),
			asm.LoadMapPtr(asm.R1, m.FD()),
			asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -8),
			asm.Mov.Imm(asm.R3, 8), asm.Mov.Imm(asm.R4, int32(notification)),
			asm.FnRingbufOutput.Call(), asm.Return(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	programInfo, err := programme.Info()
	if err != nil {
		t.Fatal(err)
	}
	programmeID, _ = programInfo.ID()
	if programmeID == 0 {
		t.Fatal("fixture programme identity unavailable")
	}
	t.Logf("owned fixture programme=%d", programmeID)
	return reader, func() {
		t.Helper()
		result, _, err := programme.Test(make([]byte, 14))
		if err != nil || result != 0 {
			t.Fatal("fixture record was not submitted", err)
		}
	}
}

func awaitFixtureAbsence(t *testing.T, open func() (io.Closer, error)) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		left, err := open()
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err != nil {
			t.Fatal("owned kernel object absence unverified", err)
		}
		if err := left.Close(); err != nil {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatal("owned kernel object outlived its cleanup bound")
		}
		// Programme release can defer dropping its map reference. Re-query the
		// exact ID within a fixed bound; closing descriptors alone is not proof.
		time.Sleep(10 * time.Millisecond)
	}
}

func TestKernelSparseFileRecordWithoutNotification(t *testing.T) {
	reader, emit := ringFixture(t, suppressWakeups)
	now := time.Now()
	// Preserve the library deadline-drain contract for explicitly unnotified fixtures.
	reader.SetDeadline(now.Add(10 * time.Millisecond))
	emit()
	var record ringbuf.Record
	if err := reader.ReadInto(&record); err != nil {
		t.Fatal("sparse record did not drain at the read deadline", err)
	}
	if len(record.RawSample) != 8 || binary.LittleEndian.Uint64(record.RawSample) != 42 {
		t.Fatal("record bytes changed")
	}
	if time.Since(now) >= 200*time.Millisecond {
		t.Fatal("sparse delivery exceeded the unchanged event latency budget")
	}
}

func TestKernelUnnotifiedBurstDrainsWithoutAnotherEvent(t *testing.T) {
	reader, emit := ringFixture(t, suppressWakeups)
	for range 128 {
		emit()
	}
	now := time.Now()
	// Preserve the library deadline-drain contract for explicitly unnotified fixtures.
	reader.SetDeadline(now.Add(10 * time.Millisecond))
	var record ringbuf.Record
	for range 128 {
		if err := reader.ReadInto(&record); err != nil {
			t.Fatal("burst lost a queued record", err)
		}
		if len(record.RawSample) != 8 || binary.LittleEndian.Uint64(record.RawSample) != 42 {
			t.Fatal("burst record bytes changed")
		}
	}
	if err := reader.ReadInto(&record); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("drained ring did not retain its deadline", err)
	}
	if time.Since(now) >= 200*time.Millisecond {
		t.Fatal("burst exceeded the unchanged event latency budget")
	}
}

func TestKernelCancellationFlushesIdleReader(t *testing.T) {
	reader, _ := ringFixture(t, suppressWakeups)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finish := interruptRead(ctx, reader)
	defer func() {
		if err := finish(); err != nil {
			t.Error("cancellation callback failed", err)
		}
	}()
	now := time.Now()
	reader.SetDeadline(now.Add(time.Second))
	cancel()
	var record ringbuf.Record
	if err := reader.ReadInto(&record); !errors.Is(err, ringbuf.ErrFlushed) {
		t.Fatal("idle reader was not directly flushed on cancellation", err)
	}
	if time.Since(now) >= 200*time.Millisecond {
		t.Fatal("cancellation waited for target validation")
	}
}

func TestKernelSparseNotifiedFileRecordCoalescesWithoutAnotherEvent(t *testing.T) {
	reader, emit := ringFixture(t, notifyOnSubmit)
	if reader.AvailableBytes() != 0 {
		t.Fatal("fixture ring did not start empty")
	}
	now := time.Now()
	validation := now.Add(time.Second)
	reader.SetDeadline(validation)
	emit()
	var record ringbuf.Record
	if err := reader.ReadInto(&record); err != nil {
		t.Fatal("submission did not wake the reader", err)
	}
	if err := coalesceFileStart(context.Background(), validation); err != nil {
		t.Fatal("sparse coalescing failed", err)
	}
	if len(record.RawSample) != 8 || binary.LittleEndian.Uint64(record.RawSample) != 42 {
		t.Fatal("sparse record bytes changed")
	}
	if time.Since(now) >= 200*time.Millisecond {
		t.Fatal("notified sparse delivery exceeded the unchanged event latency budget")
	}
}

func TestKernelNotifiedFileBurstSurvivesCoalescing(t *testing.T) {
	reader, emit := ringFixture(t, notifyOnSubmit)
	now := time.Now()
	validation := now.Add(time.Second)
	reader.SetDeadline(validation)
	emit()
	var record ringbuf.Record
	if err := reader.ReadInto(&record); err != nil {
		t.Fatal("first record unavailable", err)
	}
	if len(record.RawSample) != 8 || binary.LittleEndian.Uint64(record.RawSample) != 42 {
		t.Fatal("first record bytes changed")
	}
	for range 127 {
		emit()
	}
	if err := coalesceFileStart(context.Background(), validation); err != nil {
		t.Fatal("burst coalescing failed", err)
	}
	for range 127 {
		if err := reader.ReadInto(&record); err != nil {
			t.Fatal("queued burst record unavailable", err)
		}
		if len(record.RawSample) != 8 || binary.LittleEndian.Uint64(record.RawSample) != 42 {
			t.Fatal("burst record bytes changed")
		}
	}
	if reader.AvailableBytes() != 0 {
		t.Fatal("burst retained unread bytes")
	}
	if time.Since(now) >= 200*time.Millisecond {
		t.Fatal("notified burst exceeded the unchanged event latency budget")
	}
}
