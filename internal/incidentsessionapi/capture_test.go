package incidentsessionapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type captureFunc func(context.Context, incidentsession.Principal, string) ([]byte, error)

func (f captureFunc) CapturePod(ctx context.Context, p incidentsession.Principal, name string) ([]byte, error) {
	return f(ctx, p, name)
}

func TestCaptureFailureRemainsVisible(t *testing.T) {
	for _, test := range []struct {
		err    error
		reason string
	}{
		{incidentsession.ErrUnavailable, "source-unavailable"},
		{incidentsession.ErrCapacity, "source-capacity"},
		{incidentsession.ErrDisabled, "source-disabled"},
		{incidentsession.ErrUnsupported, "source-unsupported"},
		{nil, "source-unavailable"},
		{incidentsession.ErrDenied, "source-denied"},
		{incidentsession.ErrChanged, "source-changed"},
	} {
		t.Run(test.reason, func(t *testing.T) {
			h := testHandler(t)
			h.captures = captureFunc(func(context.Context, incidentsession.Principal, string) ([]byte, error) { return nil, test.err })
			id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
			path := collectionPath + "/" + id
			if w := request(t, h, caller("alice"), "POST", path+"/capture", `{"pod":"api"}`); w.Code != 200 {
				t.Fatalf("gap not recorded: %d", w.Code)
			}
			metadata := request(t, h, caller("alice"), "GET", path, "")
			var resource SessionResource
			if json.Unmarshal(metadata.Body.Bytes(), &resource) != nil || resource.Session.Latest.Kind != incidentsession.Gap || resource.Session.Latest.GapReason != test.reason {
				t.Fatal("failed acquisition missing from session status")
			}
			w := request(t, h, caller("alice"), "GET", path+"/export", "")
			doc, err := incidentsession.DecodeExport(w.Body.Bytes())
			if err != nil || doc.Sanitised == nil || len(doc.Sanitised.Entries) != 2 || doc.Sanitised.Entries[1].Kind != incidentsession.Gap || doc.Sanitised.Entries[1].GapReason != test.reason {
				t.Fatal("failed acquisition hidden", err)
			}
		})
	}
}

func TestCaptureChecksOwnerAndRequestBeforeAcquisition(t *testing.T) {
	h := testHandler(t)
	h.captures = captureFunc(func(context.Context, incidentsession.Principal, string) ([]byte, error) {
		t.Error("unauthorised or invalid acquisition")
		return nil, nil
	})
	id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
	path := collectionPath + "/" + id
	if w := request(t, h, caller("bob"), "POST", path+"/capture", `{"pod":"api"}`); w.Code != 404 {
		t.Fatal("other owner accepted")
	}
	for _, body := range []string{`{"pod":"../api"}`, `{"pod":"api","namespace":"team-b"}`, `{"pod":"api","digest":"forged"}`} {
		if w := request(t, h, caller("alice"), "POST", path+"/capture", body); w.Code != 400 {
			t.Fatal("invalid capture accepted")
		}
	}
	if w := request(t, h, caller("alice"), "POST", path+"/close", "{}"); w.Code != 200 {
		t.Fatal("close failed")
	}
	if w := request(t, h, caller("alice"), "POST", path+"/capture", `{"pod":"api"}`); w.Code != 409 {
		t.Fatal("closed session acquired evidence")
	}
}

func (f captureFunc) CaptureMarkers(context.Context, incidentsession.Principal, string, memoryhistory.Query) ([]byte, error) {
	return nil, incidentsession.ErrUnavailable
}
