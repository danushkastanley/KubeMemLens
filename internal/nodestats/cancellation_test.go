package nodestats

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestResponsePreservesCancellation(t *testing.T) {
	for _, stage := range []string{"headers", "body"} {
		for _, result := range []string{"valid", "invalid"} {
			t.Run(stage+"/"+result, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				body := &cancelResponseBody{Reader: strings.NewReader(`{}`)}
				client := &http.Client{Transport: responseTransport(func(*http.Request) (*http.Response, error) {
					response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
					if stage == "headers" {
						cancel()
						if result == "invalid" {
							response.Header.Del("Content-Type")
						}
					} else {
						body.cancel = cancel
						if result == "invalid" {
							body.err = io.ErrUnexpectedEOF
						}
					}
					return response, nil
				})}
				_, err := getBytes(ctx, client, "https://node.invalid/stats/summary", "", 1024)
				assertReason(t, err, nodecontext.SourceUnavailable)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				if !body.closed {
					t.Fatal("response body was not closed")
				}
			})
		}
	}
}

type responseTransport func(*http.Request) (*http.Response, error)

func (f responseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type cancelResponseBody struct {
	io.Reader
	cancel context.CancelFunc
	err    error
	closed bool
}

func (b *cancelResponseBody) Read(p []byte) (int, error) {
	if b.cancel != nil {
		b.cancel()
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.Reader.Read(p)
}

func (b *cancelResponseBody) Close() error { b.closed = true; return nil }
