package workeripc

import (
	"encoding/json"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// Plain file observations dominate the measured cached-read workload. Their
// bounded fields can be encoded without reflection while keeping the exact v1
// JSON bytes. Other messages and strings retain the standard JSON encoder.
const plainFileCapacity = 768 // 512 path bytes plus keys, timestamp and integers.

func plainFile(message responseWire) bool {
	if message.Version != Version || message.Type != "file" || message.File == nil ||
		message.Cache != nil || message.OOM != nil || message.Result != nil {
		return false
	}
	f := message.File
	if (f.Operation != trace.FileRead && f.Operation != trace.FileWrite) || len(f.Path) > 512 {
		return false
	}
	for i := range len(f.Path) {
		c := f.Path[i]
		if c < 0x20 || c >= 0x7f || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			return false
		}
	}
	return true
}

// Call only for plainFile messages with at least plainFileCapacity spare bytes.
// AppendText and MarshalJSON share time's strict RFC3339 encoding, including
// fractional seconds and offsets. An invalid timestamp remains an error.
func appendPlainFile(dst []byte, message responseWire) ([]byte, error) {
	dst = append(dst, `{"version":`...)
	dst = strconv.AppendInt(dst, int64(message.Version), 10)
	dst = append(dst, `,"type":"file","file":{"observedAt":"`...)
	stamp, err := message.File.ObservedAt.AppendText(dst)
	if err != nil {
		// AppendText returns nil on error after writing into the supplied
		// capacity. Clear that partial timestamp as well as the prefix.
		clear(dst[:cap(dst)])
		return nil, ErrProtocol
	}
	dst = stamp
	dst = append(dst, `","operation":"`...)
	dst = append(dst, message.File.Operation...)
	dst = append(dst, `","requested":`...)
	dst = strconv.AppendUint(dst, message.File.Requested, 10)
	dst = append(dst, `,"completed":`...)
	dst = strconv.AppendUint(dst, message.File.Completed, 10)
	dst = append(dst, `,"path":"`...)
	dst = append(dst, message.File.Path...)
	return append(dst, `"}}`...), nil
}

func encodeResponse(buffer *messageBuffer, encoder *json.Encoder, message responseWire) error {
	if !plainFile(message) {
		return encoder.Encode(message)
	}
	data, err := appendPlainFile(buffer.data[:4], message)
	buffer.used = len(data) // Include partially written bytes in the owner's clear.
	if err != nil {
		return err
	}
	_, err = buffer.Write([]byte{'\n'})
	return err
}
