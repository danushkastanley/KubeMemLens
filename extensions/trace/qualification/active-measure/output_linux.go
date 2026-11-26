//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"io"
)

const maxRecordBytes = 256 << 10
const maxOutputBytes = 256 << 20

type recordWriter struct {
	output  io.Writer
	written int
}

func (w *recordWriter) write(r record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxRecordBytes || w.written > maxOutputBytes-len(data) {
		return errors.New("measurement output bound reached")
	}
	n, err := w.output.Write(data)
	w.written += n
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
