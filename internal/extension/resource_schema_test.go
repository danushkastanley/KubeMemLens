package extension

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestReadSchemasKeepTenantScopeAndLegacyRepresentation(t *testing.T) {
	handler, now := populatedReadHandler(t)
	a := readContainer("team-a", "uid-a", "container-a", now)
	b := readContainer("team-b", "uid-b", "container-b", now)
	a.Context.Resources.Pod.Configured.Limit = model.ResourceValue{Bytes: 384, Known: true}
	b.Context.Resources.Pod.Configured.Limit = model.ResourceValue{Bytes: 999, Known: true}
	if _, err := handler.store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: now.Add(time.Second), Containers: []api.ContainerSnapshot{a, b}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"pods", "pods?summary=true", "pods/api", "containers", "workloads"} {
		for _, schema := range []string{"", "2"} {
			t.Run(path+"/"+schema, func(t *testing.T) {
				request := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/"+path, true)
				request.Header.Set(api.SnapshotSchemaHeader, schema)
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				if recorder.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
				}
				body := recorder.Body.String()
				if strings.Contains(body, "team-b") || strings.Contains(body, "999") {
					t.Fatal("resource extension crossed namespace scope")
				}
				if strings.Contains(body, `"resources":`) != (schema == "2") {
					t.Fatalf("wrong resource representation: %s", body)
				}
				if !strings.Contains(body, "team-a") {
					t.Fatal("identity disappeared during projection")
				}
			})
		}
	}
	if got := handler.store.ListContainers(now.Add(time.Second), time.Minute); len(got) != 2 || got[0].Context.Resources.IsZero() {
		t.Fatal("legacy read mutated retained resource context")
	}
}

func TestMalformedReadSchemaFailsBeforeStoreAccess(t *testing.T) {
	handler := NewReadHandler(nil, collector.DefaultHandlerOptions(time.Minute))
	request := readRequest(t, "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods", true)
	request.Header.Set(api.SnapshotSchemaHeader, "invalid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}
