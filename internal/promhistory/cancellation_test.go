package promhistory

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestQueryPreservesCancellationAtResponseBoundaries(t *testing.T) {
	for _, stage := range []string{"headers", "body", "decode"} {
		for _, valid := range []bool{false, true} {
			name := stage + "/invalid"
			if valid {
				name = stage + "/valid"
			}
			t.Run(name, func(t *testing.T) {
				s := fixtureSelection()
				q := fixtureQuery(s)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				data, err := json.Marshal(fixtureResponse(s, q))
				if err != nil {
					t.Fatal(err)
				}
				if stage == "decode" && !valid {
					data = []byte{0xff}
				}
				body := &cancelResponseBody{Reader: strings.NewReader(string(data))}
				c, err := New(Options{URL: "https://history.invalid", Cluster: "cluster-a"})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				c.client.Transport = responseTransport(func(*http.Request) (*http.Response, error) {
					response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
					if stage == "headers" {
						cancel()
						if !valid {
							response.Header.Del("Content-Type")
						}
					}
					if stage == "body" {
						body.cancel = cancel
						if !valid {
							body.err = io.ErrUnexpectedEOF
						}
					}
					return response, nil
				})
				calls := 0
				c.now = func() time.Time {
					calls++
					if stage == "decode" && calls == 2 {
						cancel()
					}
					return s.ResolvedAt
				}
				report, err := c.Query(ctx, s, q)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
				if !reflect.DeepEqual(report, memoryhistory.Report{}) || !body.closed || len(c.gate) != 0 {
					t.Fatal("cancelled query retained data, response body or concurrency slot")
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
