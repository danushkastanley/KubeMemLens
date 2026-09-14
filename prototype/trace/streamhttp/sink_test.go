package streamhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type pipeListener struct {
	connection net.Conn
	once       sync.Once
	closeOnce  sync.Once
	done       chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.connection })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.done); _ = l.connection.Close() })
	return nil
}
func (l *pipeListener) Addr() net.Addr { return l.connection.LocalAddr() }

func TestNonReadingHTTPClientIsStoppedByFrameDeadline(t *testing.T) {
	serverSide, client := net.Pipe()
	defer client.Close()
	listener := &pipeListener{connection: serverSide, done: make(chan struct{})}
	result := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := NewSink(w)
		if err != nil {
			result <- err
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 50*time.Millisecond)
		defer cancel()
		_, err = sink.WriteFrame(ctx, []byte(strings.Repeat("x", 4096)+"\n"))
		result <- err
	})}
	defer server.Close()
	go server.Serve(listener)
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: fixture\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	// No client read is performed. net.Pipe has no send buffer that could mask
	// backpressure, so the real HTTP flush must obey its connection deadline.
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("slow client write unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP write exceeded its deadline")
	}
}
