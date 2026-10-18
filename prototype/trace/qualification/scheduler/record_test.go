package scheduler

import (
	"encoding/binary"
	"errors"
	"testing"
)

func recordFixture(t testing.TB) ([]byte, map[uint64]Format) {
	t.Helper()
	f, err := ParseFormat(formatFixture("sched_wakeup"))
	if err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 64)
	binary.LittleEndian.PutUint32(record, 9)
	binary.LittleEndian.PutUint16(record[6:], 64)
	binary.LittleEndian.PutUint64(record[8:], 1001)
	binary.LittleEndian.PutUint64(record[16:], 500)
	binary.LittleEndian.PutUint32(record[24:], 2)
	binary.LittleEndian.PutUint32(record[32:], 28)
	binary.LittleEndian.PutUint16(record[36:], 204)
	copy(record[44:60], "private-taskname!")
	binary.LittleEndian.PutUint32(record[60:], 7)
	return record, map[uint64]Format{1001: f}
}

func TestPerfRecordChecksDescriptorCPUClockAndRawIdentity(t *testing.T) {
	raw, formats := recordFixture(t)
	e, err := Record(raw, 2, formats)
	if err != nil || e.Kind != Wake || e.PID != 7 || e.Time != 500 {
		t.Fatal(e, err)
	}
	for _, change := range []func([]byte){func(b []byte) { b[6] = 63 }, func(b []byte) { b[8] = 0 }, func(b []byte) { b[24] = 3 }, func(b []byte) { b[28] = 1 }, func(b []byte) { b[32] = 255 }, func(b []byte) { b[36] = 0 }, func(b []byte) { clear(b[16:24]) }, func(b []byte) { b[63] = 255 }} {
		data := append([]byte(nil), raw...)
		change(data)
		if _, err := Record(data, 2, formats); err == nil {
			t.Fatal("malformed sample accepted")
		}
	}
	for _, size := range []int{0, 7, 39, 63} {
		if _, err := Record(raw[:size], 2, formats); err == nil {
			t.Fatal("truncated sample accepted")
		}
	}
}

func TestLostThrottledAndUnknownRecordsCannotProduceObservations(t *testing.T) {
	raw, formats := recordFixture(t)
	for _, kind := range []uint32{2, 5, 6, 13} {
		binary.LittleEndian.PutUint32(raw, kind)
		if _, err := Record(raw, 2, formats); !errors.Is(err, ErrLoss) {
			t.Fatal("lost/throttled stream accepted")
		}
	}
	binary.LittleEndian.PutUint32(raw, 999)
	if _, err := Record(raw, 2, formats); err == nil {
		t.Fatal("unknown perf record")
	}
}

func FuzzPerfRecordProjection(f *testing.F) {
	format, err := ParseFormat(formatFixture("sched_wakeup"))
	if err != nil {
		f.Fatal(err)
	}
	validRecord, _ := recordFixture(f)
	f.Add(validRecord)
	f.Add([]byte{})
	f.Add(make([]byte, 64))
	f.Fuzz(func(t *testing.T, raw []byte) {
		e, err := Record(raw, 2, map[uint64]Format{1001: format})
		if err == nil && !valid(e) {
			t.Fatal("invalid numeric event escaped")
		}
	})
}
