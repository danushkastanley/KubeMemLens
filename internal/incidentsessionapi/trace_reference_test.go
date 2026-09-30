package incidentsessionapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func reportBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../tracereport/testdata/schema2-v2-file-loss.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func referenceBody(t *testing.T, ref tracereport.Reference) string {
	t.Helper()
	body, err := json.Marshal(incidentsession.TraceReferenceRequest{Reference: ref})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestTraceReferenceUsesExistingSmallRequestBound(t *testing.T) {
	h := testHandler(t)
	id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
	path := collectionPath + "/" + id
	data := reportBytes(t)
	data = append(data, bytes.Repeat([]byte(" "), tracereport.MaxBytes-len(data))...)
	ref, err := tracereport.Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	body := referenceBody(t, ref)
	if len(body) > MaxRequestBytes || len(data) <= MaxRequestBytes {
		t.Fatal("reference did not keep large report outside request")
	}
	w := request(t, h, caller("alice"), "POST", path+"/trace-references", body)
	if w.Code != http.StatusOK {
		t.Fatal("typed reference rejected", w.Code)
	}
	full := request(t, h, caller("alice"), "GET", path+"/export-sensitive", "")
	doc, err := incidentsession.DecodeExport(full.Body.Bytes())
	if err != nil || doc.Authorised == nil || doc.Authorised.Entries[1].TraceReference == nil || tracereport.VerifyReference(*doc.Authorised.Entries[1].TraceReference, data) != nil {
		t.Fatal("retained operator reference disagrees with local file", err)
	}
	if bytes.Contains(full.Body.Bytes(), []byte("caveats")) || len(doc.Authorised.Captures) != 0 {
		t.Fatal("report payload retained")
	}
	for _, action := range []string{"entries", "trace-references"} {
		w = request(t, h, caller("alice"), "POST", path+"/"+action, body+strings.Repeat(" ", MaxRequestBytes))
		if w.Code != http.StatusBadRequest {
			t.Fatal("existing body ceiling expanded", action, w.Code)
		}
	}
}

func TestTraceReferenceRejectsRawBodiesAndForgedAuthorityWithoutMutation(t *testing.T) {
	ref, err := tracereport.Describe(reportBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	oversized := ref
	oversized.Bytes = tracereport.MaxBytes + 1
	forged := ref
	forged.Provenance = "server-verified"
	for name, body := range map[string]string{
		"missing": "{}", "null": `{"reference":null}`, "invalid shape": `{"reference":"not a reference"}`,
		"raw report":                `{"report":"private report body"}`,
		"invalid reference":         referenceBody(t, tracereport.Reference{}),
		"oversized declared report": referenceBody(t, oversized),
		"forged source authority":   referenceBody(t, forged),
		"extra free text":           strings.TrimSuffix(referenceBody(t, ref), "}") + `,"caveats":"private path"}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := testHandler(t)
			id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
			path := collectionPath + "/" + id
			if w := request(t, h, caller("alice"), "POST", path+"/trace-references", body); w.Code != http.StatusBadRequest {
				t.Fatal("invalid reference accepted", w.Code)
			}
			w := request(t, h, caller("alice"), "GET", path, "")
			status, err := incidentsession.DecodeResource(w.Body.Bytes(), "team-a", id)
			if err != nil || status.Entries != 1 || status.LimitReached {
				t.Fatal("rejected reference changed timeline", err)
			}
		})
	}
}

func TestTraceReferenceChecksOperationOwnerAndNamespace(t *testing.T) {
	h := testHandler(t)
	id := createdID(t, request(t, h, caller("alice"), "POST", collectionPath, "{}"))
	path := collectionPath + "/" + id + "/trace-references"
	ref, err := tracereport.Describe(reportBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	body := referenceBody(t, ref)
	if w := request(t, h, caller("bob"), "POST", path, body); w.Code != http.StatusNotFound {
		t.Fatal("another actor attached reference", w.Code)
	}
	if w := request(t, h, caller("alice"), "POST", strings.Replace(path, "team-a", "team-b", 1), body); w.Code != http.StatusNotFound {
		t.Fatal("another namespace attached reference", w.Code)
	}
	h.authority.delegate = authorizer.AuthorizerFunc(func(_ context.Context, attrs authorizer.Attributes) (authorizer.Decision, string, error) {
		if attrs.GetSubresource() == incidentsession.TraceReferenceSubresource {
			return authorizer.DecisionDeny, "private reason", nil
		}
		return authorizer.DecisionAllow, "", nil
	})
	w := request(t, h, caller("alice"), "POST", path, "not valid JSON")
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "private reason") {
		t.Fatal("parsing or private errors bypassed operation permission", w.Code)
	}
}
