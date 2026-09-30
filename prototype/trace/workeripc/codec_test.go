package workeripc

import (
	"bytes"
	"strings"
	"testing"
)

func TestReceiveRequiresCanonicalResponse(t *testing.T) {
	const ready = `{"version":1,"type":"ready"}`
	const file = `{"version":1,"type":"file","file":{"observedAt":"2026-09-15T00:00:01Z","operation":"read","requested":4096,"completed":128,"path":"/fixture"}}`
	cases := map[string]string{
		"unknown":           strings.Replace(ready, `"type"`, `"extra":0,"type"`, 1),
		"duplicate":         strings.Replace(ready, `"version":1`, `"version":1,"version":1`, 1),
		"alias":             strings.Replace(ready, `"version"`, `"Version"`, 1),
		"escaped-key":       strings.Replace(ready, `"version"`, `"\u0076ersion"`, 1),
		"null":              strings.Replace(ready, `"version":1`, `"version":null`, 1),
		"omitted":           `{"type":"ready"}`,
		"trailing-document": ready + ready,
		"trailing-space":    ready + " ",
		"invalid-utf8":      strings.Replace(file, "/fixture", string([]byte{0xff}), 1),
		"nested-unknown":    strings.Replace(file, `"requested":4096`, `"extra":0,"requested":4096`, 1),
		"nested-duplicate":  strings.Replace(file, `"requested":4096`, `"requested":4096,"requested":4096`, 1),
		"nested-null":       strings.Replace(file, `"requested":4096`, `"requested":null`, 1),
		"overflow":          strings.Replace(file, `4096`, `18446744073709551616`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var message responseWire
			if _, err := receive(bytes.NewReader(packet([]byte(body))), &message); err != ErrProtocol {
				t.Fatal("non-canonical response accepted")
			}
		})
	}
	for _, body := range []string{ready, file} {
		var message responseWire
		if _, err := receive(bytes.NewReader(packet([]byte(body))), &message); err != nil {
			t.Fatal("canonical response rejected")
		}
	}
}
