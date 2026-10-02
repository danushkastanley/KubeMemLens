package workeripc

import (
	"bytes"
	"encoding/binary"
)

// Compare the canonical encoder output without allocating another message.
// Encoder adds one newline; the length-framed private wire does not include it.
// Split writes are accepted only when they match the complete payload and LF.
type canonicalMessage struct {
	remaining []byte
	complete  bool
}

func (m *canonicalMessage) Write(data []byte) (int, error) {
	n := len(data)
	if m.complete || n > len(m.remaining)+1 {
		return 0, ErrProtocol
	}
	if n == len(m.remaining)+1 {
		if data[n-1] != '\n' {
			return 0, ErrProtocol
		}
		data = data[:n-1]
		m.complete = true
	}
	if !bytes.HasPrefix(m.remaining, data) {
		return 0, ErrProtocol
	}
	m.remaining = m.remaining[len(data):]
	return n, nil
}

// One writer-owned message plus the encoder's newline. The writer mutex owns
// this storage until the synchronous pipe write finishes; it is then cleared.
type messageBuffer struct {
	data [4 + MaxMessageBytes + 1]byte
	used int
}

func (b *messageBuffer) reset() {
	clear(b.data[:b.used])
	b.used = 4
}

func (b *messageBuffer) Write(data []byte) (int, error) {
	if len(data) > len(b.data)-b.used {
		return 0, ErrProtocol
	}
	b.used += copy(b.data[b.used:], data)
	return len(data), nil
}

func (b *messageBuffer) frame() ([]byte, error) {
	if b.used <= 5 || b.data[b.used-1] != '\n' {
		return nil, ErrProtocol
	}
	size := b.used - 5
	binary.BigEndian.PutUint32(b.data[:4], uint32(size))
	return b.data[:b.used-1], nil
}
