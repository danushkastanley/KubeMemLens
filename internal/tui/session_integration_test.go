package tui

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/incidentsessionapi"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/rest"
)

type panelNamespaceFixture struct{}

func (panelNamespaceFixture) Lookup(context.Context, string) (string, error) {
	return "namespace-uid", nil
}

type panelEvidenceFixture struct{}

func (panelEvidenceFixture) CapturePod(_ context.Context, p incidentsession.Principal, name string) ([]byte, error) {
	now := time.Now().UTC()
	return json.Marshal(api.IncidentBundle{SchemaVersion: 1, CapturedAt: now, ToolVersion: "test", Pods: []api.PodSnapshot{{Namespace: p.Namespace, PodName: name, PodUID: "pod-uid", CapturedAt: now}}})
}
func (panelEvidenceFixture) CaptureMarkers(context.Context, incidentsession.Principal, string, memoryhistory.Query) ([]byte, error) {
	return nil, incidentsession.ErrDisabled
}

func liveSessionModel(t *testing.T) (*appModel, *atomic.Int32) {
	t.Helper()
	authority, err := incidentsessionapi.NewAuthority([]string{"team-a"}, panelNamespaceFixture{}, authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := incidentsessionapi.NewHandler(t.Context(), authority, incidentsession.DefaultLimits(), panelEvidenceFixture{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.Shutdown)
	var requests atomic.Int32
	factory := apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		info, err := factory.NewRequestInfo(r)
		if err != nil {
			t.Error(err)
			return
		}
		ctx := apirequest.WithRequestInfo(r.Context(), info)
		// Component-only authentication fixture; deployed request-header auth is a
		// separate cluster verification requirement.
		ctx = apirequest.WithUser(ctx, &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}})
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	config := &rest.Config{Host: server.URL, TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	reader, err := client.NewKubernetesAPIClient(config, client.AllNamespacesScope(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	m := sessionModel(t, nil)
	m.client = &sessionPanelReader{fakeSnapshotReader: tuiFixtureReader(), open: reader.OpenIncidentSessions}
	t.Cleanup(m.clearSessionPanel)
	return m, &requests
}

func completeSessionKey(t *testing.T, m *appModel, key string) {
	t.Helper()
	command := sessionKey(t, m, key)
	if command != nil {
		updated, _ := m.Update(command())
		*m = updated.(appModel)
	}
	if m.sessionPanel.err != nil {
		t.Fatal(m.sessionPanel.err)
	}
}

func pasteSession(t *testing.T, m *appModel, text string) {
	t.Helper()
	updated, _ := m.Update(tea.PasteMsg{Content: text})
	*m = updated.(appModel)
	if m.sessionPanel.err != nil {
		t.Fatal(m.sessionPanel.err)
	}
}

func TestSessionPanelTLSWorkflow(t *testing.T) {
	m, requests := liveSessionModel(t)
	completeSessionKey(t, m, "I")
	if requests.Load() != 0 {
		t.Fatal("opening panel performed implicit network work")
	}
	completeSessionKey(t, m, "n")
	if m.sessionPanel.id == "" {
		t.Fatal("session not attached after explicit start")
	}
	completeSessionKey(t, m, "t")
	pasteSession(t, m, "private decision")
	if strings.Contains(m.viewString(), "private decision") {
		t.Fatal("annotation editor exposed text")
	}
	completeSessionKey(t, m, "enter")
	completeSessionKey(t, m, "c")
	completeSessionKey(t, m, "r")
	if !strings.Contains(strings.Join(m.sessionPanel.lines, "\n"), "evidence-1") {
		t.Fatal("timeline reference missing")
	}
	completeSessionKey(t, m, "x")
	pasteSession(t, m, "evidence-1 evidence-1")
	completeSessionKey(t, m, "enter")
	if !strings.Contains(strings.Join(m.sessionPanel.lines, "\n"), "Retained evidence comparison") {
		t.Fatal("comparison result missing")
	}
	path := filepath.Join(t.TempDir(), "public.json")
	completeSessionKey(t, m, "e")
	pasteSession(t, m, path)
	completeSessionKey(t, m, "enter")
	doc, err := incident.ReadSession(path)
	if err != nil || doc.Sanitised == nil {
		t.Fatal("sanitised export unavailable", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "private decision") {
		t.Fatal("private annotation exported by default")
	}
	completeSessionKey(t, m, "z")
	if m.sessionPanel.summary.ClosedAt == nil {
		t.Fatal("session not closed")
	}
}

func TestSessionPanelDeleteIsExplicitAndFullExportProtected(t *testing.T) {
	m, _ := liveSessionModel(t)
	completeSessionKey(t, m, "I")
	completeSessionKey(t, m, "n")
	completeSessionKey(t, m, "t")
	pasteSession(t, m, "private decision")
	completeSessionKey(t, m, "enter")
	path := filepath.Join(t.TempDir(), "private.json")
	completeSessionKey(t, m, "E")
	if !strings.Contains(m.viewString(), "private annotations") {
		t.Fatal("full export disclosure missing")
	}
	pasteSession(t, m, path)
	completeSessionKey(t, m, "enter")
	doc, err := incident.ReadSession(path)
	if err != nil || doc.Authorised == nil {
		t.Fatal("full export unavailable", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private file permissions")
	}
	completeSessionKey(t, m, "d")
	if m.sessionPanel.id == "" {
		t.Fatal("delete key removed session before submission")
	}
	completeSessionKey(t, m, "esc")
	if m.sessionPanel.id == "" {
		t.Fatal("cancelled delete removed session")
	}
	completeSessionKey(t, m, "d")
	pasteSession(t, m, "delete")
	completeSessionKey(t, m, "enter")
	if m.sessionPanel.id != "" {
		t.Fatal("deleted session remained attached")
	}
}
