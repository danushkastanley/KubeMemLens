package collector

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	maxFuzzSnapshotBytes = 16 << 10
	maxFuzzDecodeBytes   = 8 << 10
)

func FuzzDecodeSnapshot(f *testing.F) {
	for _, seed := range []struct {
		data     []byte
		maxBytes uint16
	}{
		{data: nil, maxBytes: 63},
		{data: []byte(`null`), maxBytes: 63},
		{data: []byte(`{"schemaVersion":1,"nodeName":"node-a","capturedAt":"2026-08-28T00:00:00Z","environment":{},"containers":[]}`), maxBytes: 511},
		{data: []byte(`{"schemaVersion":1,"nodeName":"node-a","capturedAt":"2026-08-28T00:00:00Z","unknown":true}`), maxBytes: 511},
		{data: []byte(`{"schemaVersion":1} {}`), maxBytes: 511},
		{data: []byte(`{"schemaVersion":`), maxBytes: 511},
	} {
		f.Add(seed.data, seed.maxBytes)
	}

	f.Fuzz(func(t *testing.T, data []byte, rawMaxBytes uint16) {
		if len(data) > maxFuzzSnapshotBytes {
			return
		}

		maxBytes := int64(rawMaxBytes%maxFuzzDecodeBytes) + 1
		request := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", bytes.NewReader(data))
		got, err := decodeSnapshot(httptest.NewRecorder(), request, maxBytes)
		if err != nil {
			return
		}
		if int64(len(data)) > maxBytes {
			t.Fatalf("decode succeeded for %d bytes with a %d-byte limit", len(data), maxBytes)
		}
		if !json.Valid(data) {
			t.Fatal("decode succeeded for invalid JSON")
		}

		var typedWant api.AgentSnapshot
		if err := json.Unmarshal(data, &typedWant); err != nil {
			t.Fatalf("successful input failed typed JSON decoding: %v", err)
		}
		if !reflect.DeepEqual(got, typedWant) {
			t.Fatalf("decoded snapshot differs from encoding/json result: got=%#v want=%#v", got, typedWant)
		}

		canonical, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("marshal decoded snapshot: %v", err)
		}
		replay := httptest.NewRequest(http.MethodPost, "/api/v1/snapshots", bytes.NewReader(canonical))
		roundTripped, err := decodeSnapshot(httptest.NewRecorder(), replay, int64(len(canonical)))
		if err != nil {
			t.Fatalf("decode canonical snapshot: %v", err)
		}
		if !reflect.DeepEqual(roundTripped, got) {
			t.Fatalf("snapshot changed after canonical round trip: got=%#v want=%#v", roundTripped, got)
		}
	})
}
