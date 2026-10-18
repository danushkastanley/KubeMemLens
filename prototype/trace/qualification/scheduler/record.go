package scheduler

import (
	"encoding/binary"
	"errors"
)

var ErrLoss = errors.New("scheduler perf records lost or throttled")

// Record projects the fixed PERF_SAMPLE_IDENTIFIER|TIME|CPU|RAW layout. The
// native reader supplies only identifiers from its own descriptors for this CPU.
// No record payload or kernel task name is returned or retained.
func Record(data []byte, cpu uint32, formats map[uint64]Format) (Event, error) {
	if len(data) < 8 || len(data) > 2048 || len(data)%8 != 0 || int(binary.LittleEndian.Uint16(data[6:8])) != len(data) {
		return Event{}, ErrObservation
	}
	kind := binary.LittleEndian.Uint32(data[:4])
	switch kind {
	case 2, 5, 6, 13: // LOST, THROTTLE, UNTHROTTLE, LOST_SAMPLES.
		return Event{}, ErrLoss
	case 9: // SAMPLE.
	default:
		return Event{}, ErrObservation
	}
	if len(data) < 40 {
		return Event{}, ErrObservation
	}
	format, ok := formats[binary.LittleEndian.Uint64(data[8:16])]
	if !ok || binary.LittleEndian.Uint32(data[24:28]) != cpu || binary.LittleEndian.Uint32(data[28:32]) != 0 {
		return Event{}, ErrObservation
	}
	size := uint64(binary.LittleEndian.Uint32(data[32:36]))
	if size > 1024 || size > uint64(len(data)-36) || ((36+size+7)&^7) != uint64(len(data)) {
		return Event{}, ErrObservation
	}
	return format.Decode(data[36:36+int(size)], binary.LittleEndian.Uint64(data[16:24]))
}
