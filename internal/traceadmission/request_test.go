package traceadmission

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const validRequest = `{"schemaVersion":1,"pod":"private-pod","container":"worker","kind":"files"}`

func TestRequestDefaultsAreBoundedAndDoNotDisclosePaths(t *testing.T) {
	request, err := DecodeRequest("tenant-a", strings.NewReader(validRequest))
	if err != nil {
		t.Fatal(err)
	}
	if request.Namespace() != "tenant-a" || request.Pod() != "private-pod" || request.Container() != "worker" || request.Kind() != trace.Files {
		t.Fatal("request target changed")
	}
	if request.Paths() != trace.OmitPaths || request.Bounds() != trace.DefaultBounds() {
		t.Fatal("unexpected default limits or path disclosure")
	}
	if output := fmt.Sprintf("%+v", request); strings.Contains(output, "private-pod") || strings.Contains(output, "tenant-a") {
		t.Fatal("ordinary formatting retained target")
	}
	if _, err := json.Marshal(request); err == nil {
		t.Fatal("ordinary JSON retained target")
	}
}

func TestRequestPreservesExplicitIntentAndBounds(t *testing.T) {
	body := `{"schemaVersion":1,"pod":"pod","container":"worker","kind":"files","rawPaths":true,"durationSeconds":2,"maxEvents":1,"maxOutputBytes":1024,"maxMapBytes":4096,"maxPathBytes":64}`
	request, err := DecodeRequest("tenant-a", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if request.Paths() != trace.ConfirmedPaths || request.Bounds() != (trace.Bounds{Duration: 2 * time.Second, Events: 1, OutputBytes: 1024, MapBytes: 4096, PathBytes: 64}) {
		t.Fatal("explicit intent was changed")
	}
	for _, kind := range []trace.Kind{trace.Cache, trace.OOM} {
		body := strings.Replace(validRequest, `"files"`, `"`+string(kind)+`"`, 1)
		request, err := DecodeRequest("tenant-a", strings.NewReader(body))
		if err != nil || request.Kind() != kind {
			t.Fatalf("supported kind rejected: %v", err)
		}
	}
}

func TestRuntimeIdentityAndAmbiguousJSONAreRejected(t *testing.T) {
	fields := []string{
		`"node":"other"`, `"cgroupID":123`, `"gadget":"unreviewed"`, `"podUID":"replacement"`,
		`"containerID":"forged"`, `"containerStartedAt":"2026-01-01T00:00:00Z"`,
		`"namespace":"other-tenant"`, `"principal":"admin"`, `"selector":"app=other"`,
		`"Pod":"other"`, `"pod":"other"`, `"p\u006fd":"other"`,
		`"durationSeconds":null`, `"rawPaths":null`,
	}
	for _, field := range fields {
		body := strings.TrimSuffix(validRequest, "}") + "," + field + "}"
		if _, err := DecodeRequest("tenant-a", strings.NewReader(body)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid field accepted: %s", field)
		}
	}
	for _, body := range []string{"null", "[]", validRequest + " {}", strings.Replace(validRequest, "1,", "2,", 1), strings.Replace(validRequest, "private-pod", "..", 1), strings.Replace(validRequest, "worker", "*", 1), strings.Replace(validRequest, "files", "arbitrary", 1), strings.Replace(validRequest, "private-pod", string([]byte{0xff}), 1)} {
		if _, err := DecodeRequest("tenant-a", strings.NewReader(body)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid body accepted: %q", body)
		}
	}
	for _, namespace := range []string{"", "../tenant-a", "Tenant-A", "a/b"} {
		if _, err := DecodeRequest(namespace, strings.NewReader(validRequest)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("invalid namespace accepted")
		}
	}
}

func TestBoundsRejectZeroOverflowAndInapplicableConsent(t *testing.T) {
	for _, field := range []string{
		`"durationSeconds":0`, `"durationSeconds":301`, `"durationSeconds":18446744073709551615`,
		`"durationSeconds":-1`, `"durationSeconds":1.5`, `"maxEvents":0`, `"maxEvents":100001`,
		`"maxOutputBytes":0`, `"maxOutputBytes":33554433`, `"maxMapBytes":0`, `"maxMapBytes":33554433`,
		`"maxPathBytes":0`, `"maxPathBytes":513`,
	} {
		body := strings.TrimSuffix(validRequest, "}") + "," + field + "}"
		if _, err := DecodeRequest("tenant-a", strings.NewReader(body)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid bound accepted: %s", field)
		}
	}
	for _, kind := range []string{"cache", "oom"} {
		body := strings.TrimSuffix(strings.Replace(validRequest, "files", kind, 1), "}") + `,"rawPaths":true}`
		if _, err := DecodeRequest("tenant-a", strings.NewReader(body)); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("inapplicable raw-path consent accepted")
		}
	}
}

func TestInputReadStopsAtTheByteCeiling(t *testing.T) {
	reader := strings.NewReader(strings.Repeat(" ", MaxRequestBytes+100))
	if _, err := DecodeRequest("tenant-a", reader); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("oversized request accepted")
	}
	if reader.Len() != 99 {
		t.Fatal("decoder read beyond its bounded input")
	}
}

func FuzzDecodeRequest(f *testing.F) {
	f.Add(validRequest)
	f.Add(`{"pod":"one","pod":"two"}`)
	f.Add(`{"schemaVersion":1,"durationSeconds":18446744073709551615}`)
	f.Fuzz(func(t *testing.T, data string) {
		request, err := DecodeRequest("tenant-a", strings.NewReader(data))
		if err != nil {
			return
		}
		if err := request.Bounds().Validate(); err != nil {
			t.Fatal("accepted unbounded request")
		}
		if request.Namespace() != "tenant-a" {
			t.Fatal("body retargeted namespace")
		}
		if request.Paths() == trace.ConfirmedPaths && request.Kind() != trace.Files {
			t.Fatal("invalid consent accepted")
		}
	})
}
