package scheduler

import (
	"encoding/binary"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

const ringBytes = 256 * 1024

type perfRing struct {
	mapping  []byte
	page     *unix.PerfEventMmapPage
	data     []byte
	cpu      uint32
	formats  map[uint64]Format
	tail     uint64
	sequence uint64
}

func bindRing(mapping []byte, pageSize int, cpu uint32, formats map[uint64]Format) (*perfRing, error) {
	if pageSize < 4096 || pageSize > 65536 || pageSize&(pageSize-1) != 0 || len(mapping) != pageSize+ringBytes {
		return nil, ErrObservation
	}
	page := (*unix.PerfEventMmapPage)(unsafe.Pointer(&mapping[0]))
	if page.Compat_version != 0 || page.Data_offset != uint64(pageSize) || page.Data_size != ringBytes || atomic.LoadUint64(&page.Data_tail) != 0 {
		return nil, ErrObservation
	}
	return &perfRing{mapping: mapping, page: page, data: mapping[pageSize:], cpu: cpu, formats: formats}, nil
}

func (r *perfRing) copyAt(target []byte, offset uint64) {
	start := int(offset & uint64(len(r.data)-1))
	n := copy(target, r.data[start:])
	copy(target[n:], r.data)
}

// drain consumes one published head snapshot. Atomic head/tail operations supply
// the acquire/release ordering required by the perf mmap ABI. Scratch records
// are cleared after projection; raw payloads are never passed to the callback.
func (r *perfRing) drain(start uint64, consume func(Frame) error) (uint64, error) {
	if r.page.Data_size != uint64(len(r.data)) || r.page.Data_offset != uint64(len(r.mapping)-len(r.data)) {
		return 0, ErrObservation
	}
	head := atomic.LoadUint64(&r.page.Data_head)
	if head < r.tail || head-r.tail > uint64(len(r.data)) {
		return 0, ErrObservation
	}
	var scratch [2048]byte
	discarded := uint64(0)
	for records := 0; r.tail < head; records++ {
		if records >= 8192 || head-r.tail < 8 {
			return discarded, ErrObservation
		}
		r.copyAt(scratch[:8], r.tail)
		size := int(binary.LittleEndian.Uint16(scratch[6:8]))
		if size < 8 || size > len(scratch) || uint64(size) > head-r.tail {
			clear(scratch[:])
			return discarded, ErrObservation
		}
		r.copyAt(scratch[:size], r.tail)
		event, err := Record(scratch[:size], r.cpu, r.formats)
		clear(scratch[:size])
		if err != nil {
			return discarded, err
		}
		r.tail += uint64(size)
		atomic.StoreUint64(&r.page.Data_tail, r.tail)
		if event.Time < start {
			discarded++
			continue
		}
		r.sequence++
		if consume(Frame{Event: event, CPU: r.cpu, Sequence: r.sequence}) != nil {
			return discarded, ErrObservation
		}
	}
	return discarded, nil
}
