package traceframe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func spec(t *testing.T, kind trace.Kind, paths trace.PathPolicy) trace.Specification {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "tenant", PodName: "pod", PodUID: "pod-uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(100, 0).UTC(), NodeUID: "private-node-uid", CgroupID: 123456789}
	s, err := trace.NewSpecification(kind, target, paths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func metadata(t *testing.T) Frame {
	t.Helper()
	s := spec(t, trace.Files, trace.OmitPaths)
	now := time.Unix(200, 0).UTC()
	f, err := NewMetadata(Metadata{strings.Repeat("b", 32), "sha256:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("d", 64), s, now, now.Add(s.Bounds().Duration)})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func encoded(t *testing.T, f Frame) []byte {
	t.Helper()
	data, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestMetadataRoundTripAndPrivacy(t *testing.T) {
	f := metadata(t)
	data := encoded(t, f)
	decoded, err := Decode(data)
	if err != nil || decoded.Type() != MetadataFrame || !bytes.Equal(encoded(t, decoded), data) {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, secret := range []string{"private-node-uid", "123456789", strings.Repeat("a", 64)} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("runtime identity disclosed")
		}
	}
	if _, err := json.Marshal(f); err == nil {
		t.Fatal("frame persisted through ordinary JSON")
	}
	if strings.Contains(fmt.Sprintf("%+v", f), "tenant") {
		t.Fatal("frame disclosed in logs")
	}
}
func TestStrictDecodeRejectsAliasesDuplicatesUnknownNullAndVersions(t *testing.T) {
	data := encoded(t, metadata(t))
	cases := [][]byte{
		bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1),
		bytes.Replace(data, []byte(`"version":1`), []byte(`"Version":1`), 1),
		bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(data, []byte(`"version":1`), []byte(`"version":1,"extra":false`), 1),
		bytes.Replace(data, []byte(`"namespace":"tenant"`), []byte(`"namespace":null`), 1),
		bytes.Replace(data, []byte(`"pathBytes":256`), []byte(`"pathBytes":0`), 1),
		bytes.Replace(data, []byte(`"events":10000,`), nil, 1),
		append(data, []byte("\n")...), data[:len(data)-1], []byte("[]\n"),
	}
	for i, data := range cases {
		if _, err := Decode(data); err == nil {
			t.Errorf("malformed case %d accepted", i)
		}
	}
}
func TestDefaultPathsAreOmittedAndConfirmedControlsStayEscapedAfterJSON(t *testing.T) {
	raw := "/private/\x1b[31m\u202e\nfile"
	path, err := trace.NewSensitiveText(raw, 512)
	if err != nil {
		t.Fatal(err)
	}
	event := trace.FileActivity{ObservedAt: time.Unix(210, 0), Operation: trace.FileRead, Path: path}
	f, err := NewFile(event, spec(t, trace.Files, trace.OmitPaths))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded(t, f), []byte("private")) {
		t.Fatal("default path disclosed")
	}
	f, err = NewFile(event, spec(t, trace.Files, trace.ConfirmedPaths))
	if err != nil {
		t.Fatal(err)
	}
	var wire envelope
	if err := json.Unmarshal(encoded(t, f), &wire); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(*wire.Event.File.Path, "\x1b\n\u202e") {
		t.Fatal("JSON reader restored terminal controls")
	}
	if _, err := Decode(encoded(t, f)); err != nil {
		t.Fatal(err)
	}
	bad, _ := trace.NewSensitiveText(string([]byte{0xff}), 512)
	event.Path = bad
	if _, err := NewFile(event, spec(t, trace.Files, trace.ConfirmedPaths)); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
func TestUnknownCountsRemainExplicitNull(t *testing.T) {
	f, err := NewSummary(Summary{SessionEndedAt: time.Unix(230, 0), Termination: trace.EngineFailed, Incomplete: true})
	if err != nil {
		t.Fatal(err)
	}
	data := encoded(t, f)
	for _, key := range []string{"produced", "sampled", "lost", "rejected"} {
		if !bytes.Contains(data, []byte(`"`+key+`":null`)) {
			t.Fatal("unknown count not explicit")
		}
	}
	if _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
	missing := bytes.Replace(data, []byte(`"lost":null,`), nil, 1)
	if _, err := Decode(missing); err == nil {
		t.Fatal("missing count silently accepted")
	}
	if _, err := NewSummary(Summary{SessionEndedAt: time.Unix(230, 0), Termination: trace.Expired}); err == nil {
		t.Fatal("unknown counts claimed complete")
	}
}
func FuzzDecode(f *testing.F) {
	f.Add([]byte("{}\n"))
	f.Add([]byte(`{"version":1,"type":"event","event":{"observedAt":"2026-01-01T00:00:00Z","cache":{"operation":"add","pages":1}}}` + "\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := Decode(data)
		if err != nil {
			return
		}
		again, err := Encode(frame)
		if err != nil || !bytes.Equal(data, again) || len(again) > MaxBytes {
			t.Fatal("accepted frame changed or exceeded limit")
		}
	})
}
