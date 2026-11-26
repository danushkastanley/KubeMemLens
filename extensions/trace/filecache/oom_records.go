package filecache

import (
	"bytes"
	"encoding/binary"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// OOM decodes only victim-scoped kernel kill observations. Missing process
// context remains absent; invalid UTF-8 commands are never replaced or retained.
func (d *Decoder) OOM(data []byte) (trace.OOMDecision, error) {
	if d.spec.Kind() != trace.OOM || len(data) != 40 || !zeroBytes(data[20:24]) {
		return trace.OOMDecision{}, ErrRecord
	}
	observed, err := d.observed(binary.LittleEndian.Uint64(data[:8]))
	if err != nil {
		return trace.OOMDecision{}, ErrRecord
	}
	scope := trace.OOMScopeUnknown
	switch binary.LittleEndian.Uint32(data[8:12]) {
	case 0:
	case 1:
		scope = trace.OOMScopeCgroup
	case 2:
		scope = trace.OOMScopeGlobal
	default:
		return trace.OOMDecision{}, ErrRecord
	}
	pid, flags := binary.LittleEndian.Uint32(data[12:16]), binary.LittleEndian.Uint32(data[16:20])
	if flags > 3 || (flags&1 == 0 && pid != 0) || (flags&1 != 0 && (pid == 0 || pid > 1<<31-1)) {
		return trace.OOMDecision{}, ErrRecord
	}
	result := trace.OOMDecision{ObservedAt: observed, Scope: scope}
	if flags&1 != 0 {
		result.VictimPID = &pid
	}
	command := data[24:40]
	if flags&2 == 0 {
		if !zeroBytes(command) {
			return trace.OOMDecision{}, ErrRecord
		}
		return result, nil
	}
	end := bytes.IndexByte(command, 0)
	if end < 0 || !zeroBytes(command[end:]) {
		return trace.OOMDecision{}, ErrRecord
	}
	if !utf8.Valid(command[:end]) {
		return result, nil
	}
	result.Command, err = trace.NewSensitiveText(string(command[:end]), 16)
	if err != nil {
		return trace.OOMDecision{}, ErrRecord
	}
	return result, nil
}
