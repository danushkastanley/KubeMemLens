package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func TestIncidentTraceReferenceThroughRealTLS(t *testing.T) {
	var requests atomic.Int64
	server := incidentAPI(t)
	wire := make(chan []byte, 1)
	client, _ := incidentTLSClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "/trace-references") {
			data, err := io.ReadAll(io.LimitReader(r.Body, 4097))
			if err != nil {
				t.Error(err)
				http.Error(w, "read", 400)
				return
			}
			wire <- data
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		server.ServeHTTP(w, r)
	}))
	ctx := context.Background()
	created, err := client.StartIncident(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	if _, err := client.ReferenceIncidentTrace(ctx, created.ID, []byte(`{"secret":"not a report"}`)); !errors.Is(err, incidentsession.ErrInvalid) || requests.Load() != before {
		t.Fatal("invalid local data sent to server", err)
	}
	data, err := os.ReadFile("../tracereport/testdata/schema1-v3-oom-expiry.json")
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]any
	if json.Unmarshal(data, &original) != nil {
		t.Fatal("fixture")
	}
	original["toolVersion"] = "private-host-secret"
	original["caveats"] = []string{"private-path-secret"}
	data, err = json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, bytes.Repeat([]byte(" "), tracereport.MaxBytes-len(data))...)
	ref, err := client.ReferenceIncidentTrace(ctx, created.ID, data)
	if err != nil || ref.Latest.Kind != incidentsession.TraceReferenced {
		t.Fatal("client reference failed", err)
	}
	sent := <-wire
	if len(sent) > 4096 || bytes.Contains(sent, []byte("private-host-secret")) || bytes.Contains(sent, []byte("private-path-secret")) || bytes.Contains(sent, []byte(`"report":`)) {
		t.Fatal("report text crossed HTTP boundary")
	}
	encoded, err := client.ExportIncident(ctx, created.ID, incidentsession.ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := incidentsession.DecodeExport(encoded)
	if err != nil || doc.Authorised == nil {
		t.Fatal("trace export rejected", err)
	}
	reference, err := incidentsession.SelectTraceReference(*doc.Authorised, "trace-1")
	if err != nil || reference.ReportSchemaVersion != 1 || reference.Contract != "unreported" || tracereport.VerifyReference(reference, data) != nil {
		t.Fatal("legacy report evidence changed", err)
	}
	public, err := client.ExportIncident(ctx, created.ID, incidentsession.ExportSanitised)
	if err != nil || bytes.Contains(public, []byte(reference.Digest)) {
		t.Fatal("private reference leaked", err)
	}
	if err := client.DeleteIncident(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
}
