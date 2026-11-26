package verifier

import "encoding/binary"

// Record projects only the fixed IDENTIFIER|TID|TIME|CPU|RAW sample layout from
// descriptors already bound to the owned cgroup. Loss and throttle records fail
// before raw bytes can become evidence. Raw addresses and PID are not projected.
func Record(data []byte, cpu uint32, formats map[uint64]Format) (Event, error) {
	if len(data) < 8 || len(data) > 512 || len(data)%8 != 0 ||
		int(binary.LittleEndian.Uint16(data[6:8])) != len(data) ||
		binary.LittleEndian.Uint32(data[:4]) != 9 || len(data) < 56 {
		return Event{}, ErrObservation
	}
	format, ok := formats[binary.LittleEndian.Uint64(data[8:16])]
	pid, tid := binary.LittleEndian.Uint32(data[16:20]), binary.LittleEndian.Uint32(data[20:24])
	if !ok || pid == 0 || pid > 0x7fffffff || binary.LittleEndian.Uint32(data[32:36]) != cpu ||
		binary.LittleEndian.Uint32(data[36:40]) != 0 {
		return Event{}, ErrObservation
	}
	size := uint64(binary.LittleEndian.Uint32(data[40:44]))
	if size > 256 || size > uint64(len(data)-44) || ((44+size+7)&^7) != uint64(len(data)) {
		return Event{}, ErrObservation
	}
	return format.Decode(data[44:44+int(size)], binary.LittleEndian.Uint64(data[24:32]), tid)
}
