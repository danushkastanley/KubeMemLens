package workeripc

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func checkResponseEncoding(t testing.TB, message responseWire) {
	t.Helper()
	var buffer messageBuffer
	buffer.reset()
	err := encodeResponse(&buffer, json.NewEncoder(&buffer), message)
	want, referenceErr := encode(message)
	if (err == nil) != (referenceErr == nil) {
		t.Fatal("response encoding disagrees with standard JSON error handling")
	}
	if err == nil {
		got, err := buffer.frame()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("response encoding changed private protocol bytes")
		}
		var decoded responseWire
		if _, err := receive(bytes.NewReader(got), &decoded); err != nil {
			t.Fatal("canonical response was rejected")
		}
	}
	buffer.reset()
	if buffer.data != [4 + MaxMessageBytes + 1]byte{} {
		t.Fatal("private encoding bytes survived buffer clearing")
	}
}

func TestFileEncodingMatchesJSONForPathsTimesAndIntegerBounds(t *testing.T) {
	paths := []string{"", "/work/fixed-seed.bin", strings.Repeat("z", 512), strings.Repeat("z", 513),
		"\"\\\b\f\n\r\t\x00\x1f\x7f", "<script>&", "café/文件/\u2028\u2029", string([]byte{0xff})}
	times := []time.Time{time.Time{}, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 10, 3, 1, 2, 3, 123456789, time.UTC),
		time.Date(2026, 10, 3, 1, 2, 3, 120000000, time.FixedZone("offset", 19800)),
		time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.FixedZone("offset", -86340)),
		time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("invalid offset", 86400))}
	for _, path := range paths {
		for _, at := range times {
			for _, operation := range []trace.FileOperation{trace.FileRead, trace.FileWrite, trace.FileOpen} {
				for _, value := range []uint64{0, 1, math.MaxUint64} {
					checkResponseEncoding(t, responseWire{Version: Version, Type: "file",
						File: &fileWire{at, operation, value, value, path}})
				}
			}
		}
	}
}

func TestPlainFileEncodingStaysInItsFixedCapacity(t *testing.T) {
	message := responseWire{Version: Version, Type: "file", File: &fileWire{
		time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.FixedZone("offset", 86340)),
		trace.FileWrite, math.MaxUint64, math.MaxUint64, strings.Repeat("p", 512)}}
	if !plainFile(message) {
		t.Fatal("maximum plain file not selected")
	}
	var storage [plainFileCapacity]byte
	data, err := appendPlainFile(storage[:0], message)
	if err != nil || len(data) > len(storage) || &data[0] != &storage[0] {
		t.Fatal("file encoding exceeded its fixed storage")
	}
	clear(data)
	if storage != [plainFileCapacity]byte{} {
		t.Fatal("private comparison bytes retained")
	}
}

func TestFileCanonicalComparisonStillRejectsAlternateRepresentations(t *testing.T) {
	const valid = `{"version":1,"type":"file","file":{"observedAt":"2026-10-03T01:02:03Z","operation":"read","requested":1,"completed":0,"path":"/fixture"}}`
	for _, body := range []string{
		strings.Replace(valid, `"path":"/fixture"`, `"path":"\/fixture"`, 1),
		strings.Replace(valid, `"requested":1`, `"requested":1,"requested":1`, 1),
		strings.Replace(valid, `"requested":1`, `"requested":null`, 1),
		strings.Replace(valid, `"completed":0,`, "", 1),
		strings.Replace(valid, `"path"`, `"Path"`, 1),
		strings.Replace(valid, `"path"`, `"\u0070ath"`, 1),
		strings.Replace(valid, `"requested":1`, `"requested":1,"unknown":0`, 1),
		strings.Replace(valid, `03Z`, `03.000Z`, 1),
		valid + " ",
	} {
		var message responseWire
		if _, err := receive(bytes.NewReader(packet([]byte(body))), &message); err != ErrProtocol {
			t.Fatal("noncanonical file response accepted")
		}
	}
}

func FuzzResponseCanonicalAgreement(f *testing.F) {
	for _, seed := range []string{`{"version":1,"type":"ready"}`,
		`{"version":1,"type":"file","file":{"observedAt":"2026-10-03T01:02:03Z","operation":"read","requested":65536,"completed":65536,"path":"/work/fixed-seed.bin"}}`,
		`{"version":1,"type":"file","file":{"observedAt":"2026-10-03T01:02:03Z","operation":"write","requested":1,"completed":0,"path":"\u003c文件\u003e"}}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxMessageBytes {
			return
		}
		var reference responseWire
		valid := len(data) > 0 && utf8.Valid(data) && json.Unmarshal(data, &reference) == nil
		if valid {
			canonical, err := json.Marshal(reference)
			valid = err == nil && bytes.Equal(data, canonical)
		}
		var actual responseWire
		_, err := receive(bytes.NewReader(packet(data)), &actual)
		if (err == nil) != valid {
			t.Fatal("private response acceptance differs from canonical JSON")
		}
	})
}
