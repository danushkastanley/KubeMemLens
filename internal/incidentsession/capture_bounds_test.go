package incidentsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
)

func TestCaptureCountRetainsPriorEvidenceAndClose(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	for i := 0; i < MaxCaptures; i++ {
		if _, err := s.RecordCapture(context.Background(), p, id, captureBytes(t, p, clk.Now(), strconv.Itoa(i))); err != nil {
			t.Fatal("capture capacity smaller than documented", err)
		}
	}
	if _, err := s.RecordCapture(context.Background(), p, id, captureBytes(t, p, clk.Now(), "excess")); !errors.Is(err, ErrCapacity) {
		t.Fatal("capture count bound bypassed", err)
	}
	doc := captureExport(t, s, p, id)
	if len(doc.Captures) != MaxCaptures || len(doc.Entries) != MaxCaptures+1 || !doc.LimitReached {
		t.Fatal("capacity failure lost prior evidence")
	}
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal("capture count consumed close reserve", err)
	}
}

func FuzzPodCapture(f *testing.F) {
	f.Add([]byte(`{"schemaVersion":1,"capturedAt":"2026-09-30T10:00:00Z","toolVersion":"test","redacted":false,"pods":[{"namespace":"tenant","podName":"app","podUID":"pod-uid","capturedAt":"2026-09-30T10:00:00Z"}],"nodes":[]}`))
	f.Add([]byte(`{"pods":[null]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxCaptureBytes+1 {
			return
		}
		p := Principal{"tenant", "namespace-uid", "operator"}
		value, err := podCapture(p, data)
		if err != nil {
			return
		}
		sum := sha256.Sum256(data)
		if value.Digest != hex.EncodeToString(sum[:]) || value.NamespaceUID != p.NamespaceUID || value.SchemaVersion < 1 || value.SchemaVersion > 2 || value.ObservedAt.IsZero() || !bytes.Equal(value.Data, data) {
			t.Fatal("accepted capture lost provenance")
		}
		if len(data) > 0 {
			original := value.Data[0]
			data[0] ^= 1
			if value.Data[0] != original {
				t.Fatal("retained capture aliases input")
			}
		}
	})
}
