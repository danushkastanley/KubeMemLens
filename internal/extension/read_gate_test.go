package extension

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// stalledClient is a response writer whose client has stopped reading.
type stalledClient struct {
	*httptest.ResponseRecorder
	writing chan struct{}
	resume  chan struct{}
}

func (c *stalledClient) Write(data []byte) (int, error) {
	select {
	case c.writing <- struct{}{}:
	default:
	}
	<-c.resume
	return c.ResponseRecorder.Write(data)
}

const teamAPods = "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods"

func TestSlowClientDoesNotHoldTheReadGate(t *testing.T) {
	for name, path := range map[string]string{
		"page":    teamAPods,
		"object":  teamAPods + "/api",
		"status":  "/apis/memory.kubememlens.io/v1alpha1/clusterstatus/current",
		"missing": teamAPods + "/absent",
	} {
		t.Run(name, func(t *testing.T) {
			handler, _ := populatedReadHandler(t)
			slow := &stalledClient{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}, 1), resume: make(chan struct{})}
			slowDone := make(chan struct{})
			go func() {
				defer close(slowDone)
				handler.ServeHTTP(slow, readRequest(t, path, true))
			}()
			select {
			case <-slow.writing:
			case <-time.After(5 * time.Second):
				t.Fatal("the slow request never reached its response write")
			}

			otherDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, readRequest(t, teamAPods, true))
				otherDone <- recorder
			}()
			select {
			case other := <-otherDone:
				if other.Code != http.StatusOK {
					t.Fatalf("second read status=%d body=%s", other.Code, other.Body.String())
				}
			case <-time.After(2 * time.Second):
				close(slow.resume)
				<-slowDone
				t.Fatal("a second read waited for a client that had stopped reading")
			}
			close(slow.resume)
			<-slowDone
			if slow.Code == 0 || slow.Body.Len() == 0 || slow.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("slow response was not delivered intact: status=%d bytes=%d headers=%v", slow.Code, slow.Body.Len(), slow.Header())
			}
		})
	}
}

func TestGatedResponsesKeepStatusHeadersAndBody(t *testing.T) {
	handler, _ := populatedReadHandler(t)
	found := serveRead(t, handler, teamAPods)
	if found.Code != http.StatusOK || found.Header().Get("Content-Type") != "application/json" || found.Header().Get(api.SnapshotSchemaHeader) == "" {
		t.Fatalf("page response: status=%d headers=%v", found.Code, found.Header())
	}
	if body := found.Body.Bytes(); len(body) == 0 || body[len(body)-1] != '\n' {
		t.Fatalf("page body is not one newline-terminated JSON document: %q", body)
	}
	missing := serveRead(t, handler, teamAPods+"/absent")
	assertStatus(t, missing, http.StatusNotFound, metav1.StatusReasonNotFound)

	handler.opts.MaxResponseBytes = 16
	tooLarge := serveRead(t, handler, teamAPods)
	assertStatus(t, tooLarge, http.StatusInsufficientStorage, metav1.StatusReason("ResponseTooLarge"))
}
