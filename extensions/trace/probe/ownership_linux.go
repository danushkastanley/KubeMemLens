package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"golang.org/x/sys/unix"
)

const endedReceiptPath = "/run/kube-memlens-tracer/ended-objects.json"

// EndedReceipt is written by the trusted supervisor, never supplied by a trace
// caller. Boot ID and process start ticks bind descriptor IDs to one lifetime.
// This only audits descriptors; it does not claim a global attachment census.
type EndedReceipt struct {
	SchemaVersion   int           `json:"schemaVersion"`
	BootID          string        `json:"bootID"`
	EndedAt         time.Time     `json:"endedAt"`
	OwnerPID        int           `json:"ownerPID"`
	OwnerStartTicks uint64        `json:"ownerStartTicks"`
	Objects         []EndedObject `json:"objects"`
}
type EndedObject struct {
	FD   int    `json:"fd"`
	Kind string `json:"kind"`
	ID   uint32 `json:"id"`
	Tag  string `json:"tag,omitempty"`
}

func (l *Local) endedOwnership(ctx context.Context) (p.State, p.Reason) {
	data, err := readEndedReceipt(endedReceiptPath)
	if errors.Is(err, os.ErrNotExist) {
		return p.Supported, p.Available
	}
	if err != nil {
		return p.Degraded, p.OwnershipUncertain
	}
	boot, err := l.read("/proc/sys/kernel/random/boot_id", 64)
	if err != nil {
		return p.Degraded, p.OwnershipUncertain
	}
	receipt, err := decodeEnded(data, strings.TrimSpace(string(boot)), time.Now())
	if err != nil {
		return p.Degraded, p.OwnershipUncertain
	}
	start, err := processStart(receipt.OwnerPID)
	if errors.Is(err, os.ErrNotExist) {
		return p.Supported, p.Available
	}
	if err != nil || start != receipt.OwnerStartTicks {
		return p.Degraded, p.OwnershipUncertain
	}
	objects, err := snapshot(ctx, receipt.OwnerPID)
	if err != nil {
		return p.Degraded, p.OwnershipUncertain
	}
	after, err := processStart(receipt.OwnerPID)
	if err != nil || after != start {
		return p.Degraded, p.OwnershipUncertain
	}
	return evaluateEnded(receipt, objects)
}

func processStart(pid int) (uint64, error) {
	data, err := readBounded(fmt.Sprintf("/proc/%d/stat", pid), 4096)
	if err != nil {
		return 0, err
	}
	return parseProcessStart(data)
}

func parseProcessStart(data []byte) (uint64, error) {
	// comm may contain spaces and parentheses. The final ')' ends field 2.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, errors.New("invalid process identity")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return 0, errors.New("incomplete process identity")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, errors.New("invalid process start")
	}
	return start, nil
}

func readEndedReceipt(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Uid != 0 || st.Mode&077 != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errors.New("untrusted ownership receipt")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16<<10+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16<<10 {
		return nil, errors.New("ownership receipt exceeds bound")
	}
	return data, nil
}

func decodeEnded(data []byte, bootID string, now time.Time) (EndedReceipt, error) {
	var receipt EndedReceipt
	invalid := errors.New("invalid ownership receipt")
	if len(data) > 16<<10 {
		return receipt, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil {
		return receipt, invalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || receipt.SchemaVersion != 1 || receipt.BootID == "" || len(receipt.BootID) > 64 || receipt.BootID != bootID || receipt.EndedAt.IsZero() || receipt.EndedAt.After(now) || receipt.OwnerPID <= 0 || receipt.OwnerPID > 1<<22 || receipt.OwnerStartTicks == 0 || len(receipt.Objects) == 0 || len(receipt.Objects) > 64 {
		return receipt, invalid
	}
	seen := map[int]bool{}
	for _, entry := range receipt.Objects {
		if entry.FD < 0 || entry.FD > 1<<20 || entry.ID == 0 || seen[entry.FD] {
			return receipt, invalid
		}
		switch entry.Kind {
		case "prog":
			if !validTag(entry.Tag) {
				return receipt, invalid
			}
		case "map", "link":
			if entry.Tag != "" {
				return receipt, invalid
			}
		default:
			return receipt, invalid
		}
		seen[entry.FD] = true
	}
	return receipt, nil
}

func evaluateEnded(receipt EndedReceipt, objects map[int]object) (p.State, p.Reason) {
	found := false
	for _, entry := range receipt.Objects {
		observed, exists := objects[entry.FD]
		if !exists {
			continue
		}
		if observed != (object{kind: entry.Kind, id: entry.ID, tag: entry.Tag}) {
			return p.Degraded, p.OwnershipUncertain
		}
		found = true
	}
	if found {
		return p.Unsupported, p.OwnedOrphan
	}
	return p.Supported, p.Available
}
