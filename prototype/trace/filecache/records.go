package filecache

import (
	"encoding/binary"
	"errors"
	"math/bits"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

var ErrRecord = errors.New("invalid file/cache observation")

// Alignment binds one monotonic observation interval to the node's wall clock.
// The worker must measure uncertainty and recheck drift before correlation.
type Alignment struct {
	MonotonicNS uint64
	WallTime    time.Time
	Duration    time.Duration
	Uncertainty time.Duration
}

type Decoder struct {
	spec  trace.Specification
	clock Alignment
}

func NewDecoder(spec trace.Specification, clock Alignment) (*Decoder, error) {
	if spec.Validate() != nil || !validProgrammeKind(spec.Kind()) ||
		clock.MonotonicNS == 0 || clock.WallTime.IsZero() || clock.Duration <= 0 ||
		clock.Duration > spec.Bounds().Duration || clock.Uncertainty < 0 || clock.Uncertainty > 5*time.Millisecond {
		return nil, ErrRecord
	}
	clock.WallTime = clock.WallTime.UTC()
	return &Decoder{spec, clock}, nil
}

func (d *Decoder) observed(ns uint64) (time.Time, error) {
	if ns < d.clock.MonotonicNS || ns-d.clock.MonotonicNS > uint64(5*time.Minute) {
		return time.Time{}, ErrRecord
	}
	elapsed := time.Duration(ns - d.clock.MonotonicNS)
	if elapsed > d.clock.Duration {
		return time.Time{}, ErrRecord
	}
	return d.clock.WallTime.Add(elapsed), nil
}

// File rejects any nonzero path bytes under omit policy, including bytes past
// the declared string length. Raw ABI bytes must never be logged on failure.
func (d *Decoder) File(data []byte) (trace.FileActivity, error) {
	if d.spec.Kind() != trace.Files || len(data) != 552 {
		return trace.FileActivity{}, ErrRecord
	}
	observed, err := d.observed(binary.LittleEndian.Uint64(data[:8]))
	if err != nil {
		return trace.FileActivity{}, err
	}
	requested, completed := binary.LittleEndian.Uint64(data[8:16]), binary.LittleEndian.Uint64(data[16:24])
	if completed > requested || !zeroBytes(data[545:]) {
		return trace.FileActivity{}, ErrRecord
	}
	var operation trace.FileOperation
	switch binary.LittleEndian.Uint32(data[24:28]) {
	case 1:
		operation = trace.FileRead
	case 2:
		operation = trace.FileWrite
	default:
		return trace.FileActivity{}, ErrRecord
	}
	length := binary.LittleEndian.Uint32(data[28:32])
	if length > 512 || uint64(length) > d.spec.Bounds().PathBytes || !zeroBytes(data[32+length:545]) {
		return trace.FileActivity{}, ErrRecord
	}
	if d.spec.Paths() == trace.OmitPaths && length != 0 {
		return trace.FileActivity{}, ErrRecord
	}
	if d.spec.Paths() == trace.ConfirmedPaths && length == 0 {
		return trace.FileActivity{}, ErrRecord
	}
	raw := data[32 : 32+length]
	if !utf8.Valid(raw) || containsZero(raw) {
		return trace.FileActivity{}, ErrRecord
	}
	path, err := trace.NewSensitiveText(string(raw), d.spec.Bounds().PathBytes)
	if err != nil {
		return trace.FileActivity{}, ErrRecord
	}
	return trace.FileActivity{ObservedAt: observed, Operation: operation, RequestedBytes: &requested, CompletedBytes: &completed, Path: path}, nil
}

func (d *Decoder) Cache(data []byte) (trace.CacheActivity, error) {
	if d.spec.Kind() != trace.Cache || len(data) != 24 || !zeroBytes(data[20:]) {
		return trace.CacheActivity{}, ErrRecord
	}
	observed, err := d.observed(binary.LittleEndian.Uint64(data[:8]))
	if err != nil {
		return trace.CacheActivity{}, err
	}
	pages := binary.LittleEndian.Uint64(data[8:16])
	// The programme reports 2^folio_order base pages, never arbitrary byte sizes.
	if pages == 0 || pages > 1<<62 || pages&(pages-1) != 0 {
		return trace.CacheActivity{}, ErrRecord
	}
	var operation trace.CacheOperation
	switch binary.LittleEndian.Uint32(data[16:20]) {
	case 1:
		operation = trace.CacheAdd
	case 2:
		operation = trace.CacheRemove
	default:
		return trace.CacheActivity{}, ErrRecord
	}
	return trace.CacheActivity{ObservedAt: observed, Operation: operation, Pages: pages}, nil
}

func DecodeCounts(data []byte) (trace.Counts, error) {
	if len(data) != 32 {
		return trace.Counts{}, ErrRecord
	}
	produced := binary.LittleEndian.Uint64(data[:8])
	sampled := binary.LittleEndian.Uint64(data[8:16])
	lost := binary.LittleEndian.Uint64(data[16:24])
	rejected := binary.LittleEndian.Uint64(data[24:32])
	total, carry := bits.Add64(sampled, lost, 0)
	if carry != 0 {
		return trace.Counts{}, ErrRecord
	}
	total, carry = bits.Add64(total, rejected, 0)
	if carry != 0 || total > produced {
		return trace.Counts{}, ErrRecord
	}
	return trace.Counts{Produced: &produced, Sampled: &sampled, Lost: &lost, Rejected: &rejected}, nil
}

func zeroBytes(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
func containsZero(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
