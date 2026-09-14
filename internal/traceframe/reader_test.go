package traceframe

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func stream(t *testing.T) []byte {
	t.Helper()
	data := encoded(t, metadata(t))
	event, err := NewFile(trace.FileActivity{ObservedAt: time.Unix(210, 0), Operation: trace.FileRead}, spec(t, trace.Files, trace.OmitPaths))
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, encoded(t, event)...)
	start, end := time.Unix(200, 0).UTC(), time.Unix(230, 0).UTC()
	one, zero := uint64(1), uint64(0)
	summary, err := NewSummary(Summary{SessionEndedAt: end, ObservationStartedAt: &start, ObservationEndedAt: &end, Termination: trace.Expired, EngineCounts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, WrittenEvents: 1, WrittenBytesBeforeSummary: uint64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	return append(data, encoded(t, summary)...)
}
func consume(data []byte) (*Reader, error) {
	reader := NewReader(bytes.NewReader(data))
	for {
		_, err := reader.Next()
		if err != nil {
			return reader, err
		}
	}
}
func TestReaderValidatesCompleteStreamAndActualBytes(t *testing.T) {
	data := stream(t)
	reader, err := consume(data)
	if err != io.EOF {
		t.Fatal(err)
	}
	n, events := reader.Counts()
	if n != uint64(len(data)) || events != 1 {
		t.Fatal("stream counts differ from wire")
	}
}
func TestReaderRejectsMissingSummaryExtraFramesAndChangedAccounting(t *testing.T) {
	data := stream(t)
	lines := bytes.SplitAfter(data, []byte("\n"))
	cases := [][]byte{nil, lines[1], append(append([]byte{}, lines[0]...), lines[1]...), append(append([]byte{}, data...), lines[1]...), data[:len(data)-1], bytes.Replace(data, []byte(`"writtenEvents":1`), []byte(`"writtenEvents":0`), 1), bytes.Replace(data, []byte(`"paths":"omit"`), []byte(`"paths":"confirmed"`), 1), bytes.Replace(data, []byte(`"kind":"files"`), []byte(`"kind":"cache"`), 1)}
	for i, data := range cases {
		_, err := consume(data)
		if err == io.EOF {
			t.Errorf("invalid stream %d claimed complete", i)
		}
	}
	oversized := append(encoded(t, metadata(t)), []byte(strings.Repeat("x", MaxBytes+1)+"\n")...)
	if _, err := consume(oversized); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("oversized frame was not bounded: %v", err)
	}
}
func TestVisibleEscapingIsUnambiguousAndPreservesEffectiveByteBounds(t *testing.T) {
	for _, raw := range []string{"/safe/path", `/literal/\u{001b}`, "/escape/\x1b[31m", string([]byte{0, 1, 2}), "unicode/é\u202e"} {
		encoded, err := SafeText(raw, 512)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeText(encoded, 512)
		if err != nil || decoded != raw {
			t.Fatalf("text round trip: %v", err)
		}
		if len(raw) > 1 {
			if _, err := DecodeText(encoded, uint64(len(raw)-1)); err == nil {
				t.Fatal("effective raw byte limit bypassed by escaping")
			}
		}
	}
	for _, bad := range []string{`\u{0061}`, `\u{D800}`, `\u{110000}`, `\x1b`, "\x1b", `\u{1b}`, strings.Repeat("a", 513)} {
		if _, err := DecodeText(bad, 512); err == nil {
			t.Fatal("noncanonical or oversized text accepted")
		}
	}
}
