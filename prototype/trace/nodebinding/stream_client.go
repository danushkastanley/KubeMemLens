package nodebinding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

// StreamBinding is private node transport, only reachable through an owned
// admission Lease. Streams have separate connections from control and cleanup.
type StreamBinding interface {
	admission.Binding
	OpenStream(context.Context, time.Time) (io.ReadCloser, error)
}
type streamBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *streamBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

func (b *binding) OpenStream(parent context.Context, deadline time.Time) (io.ReadCloser, error) {
	b.mu.Lock()
	if b.closed || b.claimed || !time.Now().Before(b.expires) || !deadline.After(time.Now()) || deadline.After(time.Now().Add(5*time.Minute)) {
		b.mu.Unlock()
		return nil, admission.ErrExpired
	}
	b.claimed = true
	b.mu.Unlock()
	ctx, cancel := context.WithDeadline(parent, deadline.Add(2*time.Second))
	data, err := json.Marshal(streamRequest{Deadline: deadline})
	if err != nil {
		cancel()
		return nil, admission.ErrUnavailable
	}
	// A non-replayable body prevents net/http from retrying a lost POST.
	body := struct{ io.Reader }{bytes.NewReader(data)}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.node.url+"/v1/bindings/"+b.id+"/stream", body)
	if err != nil {
		cancel()
		return nil, admission.ErrUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(controllerHeader, b.node.owner)
	request.Header.Set(nodeHeader, b.instance)
	response, err := b.node.stream.Do(request)
	if err != nil {
		cancel()
		return nil, admission.ErrUnavailable
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/x-ndjson" {
		response.Body.Close()
		cancel()
		return nil, admission.ErrUnavailable
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		response.Body.Close()
		cancel()
		return nil, admission.ErrExpired
	}
	b.expires = deadline
	b.mu.Unlock()
	return &streamBody{response.Body, cancel}, nil
}
