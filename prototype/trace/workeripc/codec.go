// Package workeripc defines the private, single-session pipe protocol between
// the optional node service and its supervised worker. It loads no BPF code.
package workeripc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const Version = 1
const MaxMessageBytes = 4096

var ErrProtocol = errors.New("invalid private worker protocol")
var ErrOutput = errors.New("private worker output failed")
var ErrLimit = errors.New("private worker output limit reached")

// Messages have a four-byte big-endian length followed by canonical JSON.
// Canonical comparison rejects aliases, duplicate keys, missing fields, null
// substitutions and unknown fields. These bytes must never be logged or saved.
func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || len(data) > MaxMessageBytes {
		return nil, ErrProtocol
	}
	message := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(message, uint32(len(data)))
	copy(message[4:], data)
	return message, nil
}

func receive(r io.Reader, value any) (int, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, ErrProtocol
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > MaxMessageBytes {
		return 0, ErrProtocol
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(r, data); err != nil || !utf8.Valid(data) {
		return 0, ErrProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return 0, ErrProtocol
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(data, canonical) {
		return 0, ErrProtocol
	}
	return len(data) + 4, nil
}

func send(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err != nil || n != len(data) {
		return ErrOutput
	}
	return nil
}

// EOF is mandatory after the one request or terminal result. The process owner
// must impose a pipe deadline and kill/reap a child that keeps the pipe open.
func end(r io.Reader) error {
	var extra [1]byte
	n, err := r.Read(extra[:])
	if n != 0 || err != io.EOF {
		return ErrProtocol
	}
	return nil
}
