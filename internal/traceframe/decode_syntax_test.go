package traceframe

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

const canonicalFileEvent = `{"version":2,"type":"event","event":{"observedAt":"2026-09-29T00:00:00Z","file":{"operation":"read","requestedBytes":4096,"completedBytes":128,"path":"/fixture"}}}`

func assertSameSyntax(t *testing.T, data []byte) {
	t.Helper()
	fast, fastErr := decodeFrameSyntax(data)
	strict, strictErr := decodeStrictSyntax(data)
	if (fastErr == nil) != (strictErr == nil) {
		t.Fatal("optimised and strict parsers disagree on acceptance")
	}
	if fastErr != nil {
		return
	}
	a, errA := json.Marshal(fast)
	b, errB := json.Marshal(strict)
	if errA != nil || errB != nil || !bytes.Equal(a, b) {
		t.Fatal("optimised and strict parsers disagree on typed values")
	}
}

func TestEventSyntaxPreservesStrictRejections(t *testing.T) {
	cases := map[string]string{
		"valid":            canonicalFileEvent,
		"unknown":          strings.Replace(canonicalFileEvent, `"path":`, `"extra":0,"path":`, 1),
		"misplaced-known":  strings.Replace(canonicalFileEvent, `"path":`, `"scope":"cgroup","path":`, 1),
		"alias":            strings.Replace(canonicalFileEvent, `"operation"`, `"Operation"`, 1),
		"duplicate":        strings.Replace(canonicalFileEvent, `"operation":"read"`, `"operation":"read","operation":"read"`, 1),
		"missing-nullable": strings.Replace(canonicalFileEvent, `"requestedBytes":4096,`, "", 1),
		"explicit-null":    strings.Replace(canonicalFileEvent, `"requestedBytes":4096`, `"requestedBytes":null`, 1),
		"null-path":        strings.Replace(canonicalFileEvent, `"path":"/fixture"`, `"path":null`, 1),
		"null-event":       `{"version":2,"type":"event","event":null}`,
		"trailing-value":   canonicalFileEvent + ` {}`,
		"extra-union":      strings.Replace(canonicalFileEvent, `"file":`, `"cache":{"operation":"add","pages":1},"file":`, 1),
		"overflow":         strings.Replace(canonicalFileEvent, `4096`, `18446744073709551616`, 1),
		"whitespace":       strings.ReplaceAll(canonicalFileEvent, ":", ": "),
		"reordered":        strings.Replace(canonicalFileEvent, `"version":2,"type":"event"`, `"type":"event","version":2`, 1),
		"escaped-key":      strings.Replace(canonicalFileEvent, `"type"`, `"\u0074ype"`, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) { assertSameSyntax(t, []byte(body+"\n")) })
	}
	var e envelope
	data := []byte(canonicalFileEvent + "\n")
	if json.Unmarshal(data, &e) != nil || !canonicalEvent(data, e) {
		t.Fatal("normal production event missed the canonical path")
	}
}

func TestNoncanonicalEventPreservesReceivedBytes(t *testing.T) {
	for _, body := range []string{
		strings.Replace(canonicalFileEvent, `"version":2,"type":"event"`, `"type":"event", "version":2`, 1),
		canonicalFileEvent + " ",
	} {
		data := []byte(body + "\n")
		frame, err := Decode(data)
		if err != nil {
			t.Fatal("legal noncanonical event rejected")
		}
		encoded, err := Encode(frame)
		if err != nil || !bytes.Equal(data, encoded) {
			t.Fatal("forwarding changed received bytes or accounting")
		}
	}
}

func FuzzEventSyntaxMatchesStrictDecoder(f *testing.F) {
	f.Add([]byte(canonicalFileEvent))
	f.Add([]byte(strings.Replace(canonicalFileEvent, `4096`, `null`, 1)))
	f.Add([]byte(`{"version":3,"type":"event","event":{"observedAt":"2026-09-29T00:00:00Z","oom":{"scope":"cgroup","victimPID":1,"command":"fixture"}}}`))
	f.Add([]byte(`{"version":1,"type":"event","event":{"observedAt":"2026-09-29T00:00:00Z","cache":{"operation":"add","pages":1}}}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) == 0 || len(body) >= MaxBytes || !utf8.Valid(body) || bytes.ContainsRune(body, '\n') {
			return
		}
		data := append(bytes.Clone(body), '\n')
		assertSameSyntax(t, data)
	})
}
