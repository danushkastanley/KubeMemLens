package workeripc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func requestFixture(t testing.TB, kind trace.Kind, paths trace.PathPolicy, bounds trace.Bounds) Request {
	t.Helper()
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	spec, err := trace.NewSpecification(kind, trace.TargetIdentity{Namespace: "tenant", PodName: "private-pod", PodUID: "pod-uid", ContainerName: "work", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: now.Add(-time.Minute), NodeUID: "node-uid", CgroupID: 123}, paths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	return Request{spec, now, now.Add(bounds.Duration), strings.Repeat("b", 64)}
}

func fileFixture(r Request, path string) trace.FileActivity {
	text, _ := trace.NewSensitiveText(path, r.Specification.Bounds().PathBytes)
	requested, completed := uint64(4096), uint64(128)
	return trace.FileActivity{ObservedAt: r.IssuedAt.Add(time.Second), Operation: trace.FileRead, RequestedBytes: &requested, CompletedBytes: &completed, Path: text}
}

func resultFixture(r Request) trace.Result {
	return trace.Result{Version: trace.ContractVersion, StartedAt: r.IssuedAt, EndedAt: r.Deadline, Termination: trace.Expired, Incomplete: true}
}

type capture struct {
	files []trace.FileActivity
	cache []trace.CacheActivity
	fail  bool
}

func (c *capture) FileActivity(e trace.FileActivity) error {
	if c.fail {
		return errors.New("private output error")
	}
	c.files = append(c.files, e)
	return nil
}
func (c *capture) CacheActivity(e trace.CacheActivity) error {
	c.cache = append(c.cache, e)
	return nil
}
func (*capture) OOMDecision(trace.OOMDecision) error { return ErrProtocol }

func TestRequestRoundTripAndRedaction(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	var data bytes.Buffer
	if err := WriteRequest(&data, r); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(&data)
	if err != nil || got != r {
		t.Fatal("frozen request changed")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", r, r), "private-pod") {
		t.Fatal("ordinary formatting exposed target")
	}
	if _, err := json.Marshal(r); err == nil {
		t.Fatal("ordinary JSON exposed target")
	}
}

func packet(data []byte) []byte {
	out := make([]byte, len(data)+4)
	binary.BigEndian.PutUint32(out, uint32(len(data)))
	copy(out[4:], data)
	return out
}

func TestRequestRejectsAmbiguousMalformedAndUnboundedInput(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	var original bytes.Buffer
	if err := WriteRequest(&original, r); err != nil {
		t.Fatal(err)
	}
	valid := original.Bytes()
	body := string(valid[4:])
	cases := map[string][]byte{
		"duplicate":      packet([]byte(strings.Replace(body, `"version":1`, `"version":1,"version":1`, 1))),
		"alias":          packet([]byte(strings.Replace(body, `"kind"`, `"Kind"`, 1))),
		"null":           packet([]byte(strings.Replace(body, `"cgroupID":123`, `"cgroupID":null`, 1))),
		"unknown":        packet([]byte(strings.Replace(body, `"version":1`, `"version":1,"gadget":"arbitrary"`, 1))),
		"omitted":        packet([]byte(strings.Replace(body, `"pathBytes":256,`, "", 1))),
		"future-version": packet([]byte(strings.Replace(body, `"version":1`, `"version":2`, 1))),
		"truncated":      valid[:len(valid)-1],
		"second-request": append(append([]byte(nil), valid...), valid...),
		"oversized":      {0xff, 0xff, 0xff, 0xff},
		"empty":          {0, 0, 0, 0},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadRequest(bytes.NewReader(data)); !errors.Is(err, ErrProtocol) {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestFileAndCacheTransportPreservesTypedMeaning(t *testing.T) {
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		t.Run(string(kind), func(t *testing.T) {
			r := requestFixture(t, kind, trace.OmitPaths, trace.DefaultBounds())
			var data bytes.Buffer
			w, _ := NewWriter(&data, r)
			if err := w.Ready(); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == trace.Files {
				err = w.FileActivity(fileFixture(r, ""))
			} else {
				err = w.CacheActivity(trace.CacheActivity{ObservedAt: r.IssuedAt, Operation: trace.CacheAdd, Pages: 512})
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Finish(resultFixture(r)); err != nil {
				t.Fatal(err)
			}
			var got capture
			ready := 0
			result, err := ReadStream(&data, r, &got, func() { ready++ })
			if err != nil || ready != 1 || result.Termination != trace.Expired {
				t.Fatal("stream did not complete")
			}
			if kind == trace.Files && (len(got.files) != 1 || *got.files[0].RequestedBytes != 4096 || *got.files[0].CompletedBytes != 128 || got.files[0].Path.Reveal() != "") {
				t.Fatal("file semantics changed")
			}
			if kind == trace.Cache && (len(got.cache) != 1 || got.cache[0].Pages != 512 || got.cache[0].Operation != trace.CacheAdd) {
				t.Fatal("cache semantics changed")
			}
		})
	}
}

func TestConfirmedPathsRemainExactOnlyInExplicitPipe(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.ConfirmedPaths, trace.DefaultBounds())
	var data bytes.Buffer
	w, _ := NewWriter(&data, r)
	path := "/private/\x1b[31m\\secret\u202e"
	if err := w.Ready(); err != nil {
		t.Fatal(err)
	}
	if err := w.FileActivity(fileFixture(r, path)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%#v", w), "secret") {
		t.Fatal("writer leaked path")
	}
	if _, err := json.Marshal(w); err == nil {
		t.Fatal("writer serialised")
	}
	if err := w.Finish(resultFixture(r)); err != nil {
		t.Fatal(err)
	}
	var got capture
	if _, err := ReadStream(&data, r, &got, func() {}); err != nil {
		t.Fatal(err)
	}
	if len(got.files) != 1 || got.files[0].Path.Reveal() != path {
		t.Fatal("private path changed before public escaping")
	}
}

func TestOutputLimitsRemainStickyAndReserveTerminal(t *testing.T) {
	for _, limit := range []string{"events", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			bounds := trace.DefaultBounds()
			if limit == "events" {
				bounds.Events = 1
			} else {
				bounds.OutputBytes = 1
			}
			r := requestFixture(t, trace.Files, trace.OmitPaths, bounds)
			var data bytes.Buffer
			w, _ := NewWriter(&data, r)
			if err := w.Ready(); err != nil {
				t.Fatal(err)
			}
			if limit == "events" {
				if err := w.FileActivity(fileFixture(r, "")); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := w.FileActivity(fileFixture(r, "")); err != ErrLimit {
					t.Fatal("limit not sticky")
				}
			}
			result := resultFixture(r)
			result.Termination = trace.OutputLimit
			if err := w.Finish(result); err != nil {
				t.Fatal("terminal reserve lost")
			}
			if _, err := ReadStream(&data, r, &capture{}, func() {}); err != nil {
				t.Fatal(err)
			}
			if err := w.Ready(); err == nil {
				t.Fatal("finished writer reused")
			}
		})
	}
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

func TestWriteFailureNeverRetriesOrProducesTerminal(t *testing.T) {
	r := requestFixture(t, trace.Files, trace.OmitPaths, trace.DefaultBounds())
	w, _ := NewWriter(shortWriter{}, r)
	if w.Ready() != ErrOutput || w.Finish(resultFixture(r)) == nil {
		t.Fatal("short write hidden")
	}
	if WriteRequest(shortWriter{}, r) != ErrOutput {
		t.Fatal("short request write hidden")
	}
}

func FuzzRequestDecode(f *testing.F) {
	var valid bytes.Buffer
	if err := WriteRequest(&valid, requestFixture(f, trace.Files, trace.OmitPaths, trace.DefaultBounds())); err != nil {
		f.Fatal(err)
	}
	f.Add(valid.Bytes())
	f.Add([]byte{0, 0, 0, 2, '{', '}'})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := ReadRequest(bytes.NewReader(data))
		if err == nil && r.Validate() != nil {
			t.Fatal("invalid specification accepted")
		}
	})
}

var _ io.Writer = shortWriter{}
