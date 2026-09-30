package incidentsessionapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionSlowBodyReleasesAdmission(t *testing.T) {
	h := testHandler(t)
	finished := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
		defer cancel()
		h.ServeHTTP(w, withRequestContext(t, r.WithContext(ctx), caller("alice")))
	}))
	defer server.Close()
	// Use the server's trusted test CA and leave the declared body incomplete.
	transport := server.Client().Transport.(*http.Transport)
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), transport.TLSClientConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: localhost\r\nContent-Length: 2\r\nContent-Type: application/json\r\n%s: 1\r\n\r\n{", collectionPath, SchemaHeader); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("slow body retained handler after request deadline")
	}
	if len(h.gate) != 0 {
		t.Fatal("slow body retained admission slot")
	}
}

func TestSessionRequiresBoundedTransport(t *testing.T) {
	h := testHandler(t)
	r := httptest.NewRequest("POST", collectionPath, strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(SchemaHeader, "1")
	w := httptest.NewRecorder() // deliberately lacks transport deadline support
	h.ServeHTTP(w, withRequestContext(t, r, caller("alice")))
	if w.Code != 503 {
		t.Fatal("unbounded transport accepted")
	}
}
