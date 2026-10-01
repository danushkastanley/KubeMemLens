package scheduler

import (
	"encoding/binary"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func testRing(t *testing.T) *perfRing {
	t.Helper()
	pageSize := os.Getpagesize()
	data, err := unix.Mmap(-1, 0, pageSize+ringBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(data); err != nil {
			t.Error(err)
		}
	})
	page := (*unix.PerfEventMmapPage)(unsafe.Pointer(&data[0]))
	page.Data_offset = uint64(pageSize)
	page.Data_size = ringBytes
	_, formats := recordFixture(t)
	ring, err := bindRing(data, pageSize, 2, formats)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func putRecord(r *perfRing, record []byte, offset uint64) {
	for i, value := range record {
		r.data[(offset+uint64(i))%uint64(len(r.data))] = value
	}
	atomic.StoreUint64(&r.page.Data_head, offset+uint64(len(record)))
}

func TestRingWrapStartupDiscardAndReleaseTail(t *testing.T) {
	r := testRing(t)
	raw, _ := recordFixture(t)
	r.tail = ringBytes - 16
	atomic.StoreUint64(&r.page.Data_tail, r.tail)
	putRecord(r, raw, r.tail)
	discarded, err := r.drain(600, func(Frame) error { t.Fatal("setup event escaped"); return nil })
	if err != nil || discarded != 1 || r.sequence != 0 {
		t.Fatal(discarded, err)
	}
	binary.LittleEndian.PutUint64(raw[16:], 700)
	putRecord(r, raw, r.tail)
	seen := 0
	discarded, err = r.drain(600, func(f Frame) error {
		seen++
		if f.Sequence != 1 || f.CPU != 2 || f.Event.Time != 700 {
			t.Fatal(f)
		}
		return nil
	})
	if err != nil || discarded != 0 || seen != 1 || atomic.LoadUint64(&r.page.Data_tail) != r.tail {
		t.Fatal(discarded, seen, err)
	}
}

func TestRingOverrunTruncationAndLostRecordsFail(t *testing.T) {
	for _, kind := range []string{"overrun", "header", "record", "loss", "callback"} {
		t.Run(kind, func(t *testing.T) {
			r := testRing(t)
			raw, _ := recordFixture(t)
			putRecord(r, raw, 0)
			switch kind {
			case "overrun":
				atomic.StoreUint64(&r.page.Data_head, ringBytes+1)
			case "header":
				atomic.StoreUint64(&r.page.Data_head, 7)
			case "record":
				atomic.StoreUint64(&r.page.Data_head, 40)
			case "loss":
				binary.LittleEndian.PutUint32(r.data, 2)
			}
			_, err := r.drain(1, func(Frame) error {
				if kind == "callback" {
					return ErrObservation
				}
				return nil
			})
			if err == nil {
				t.Fatal("incomplete ring accepted")
			}
			if kind == "loss" && !errors.Is(err, ErrLoss) {
				t.Fatal("loss category hidden")
			}
		})
	}
}

func TestCaptureCloseStillClosesAllFDsAfterDisableFailure(t *testing.T) {
	var pipes [2]int
	if err := unix.Pipe2(pipes[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	c := &Capture{fds: pipes[:]}
	if c.Close() == nil {
		t.Fatal("non-perf disable failure hidden")
	}
	for _, fd := range pipes {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatal("descriptor retained", err)
		}
	}
	if c.Close() == nil {
		t.Fatal("failed cleanup became success on repeated close")
	}
	if c.Drain(func(Frame) error { return nil }) == nil {
		t.Fatal("closed capture reused")
	}
}
