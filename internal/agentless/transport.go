package agentless

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

var (
	errResponseLimit = errors.New("agentless response byte limit reached")
	errTotalLimit    = errors.New("agentless refresh byte limit reached")
	errRequestLimit  = errors.New("agentless refresh request limit reached")
	errMissingBudget = errors.New("agentless request has no refresh budget")
)

type budgetKey struct{}

type readBudget struct {
	bytes    atomic.Int64
	requests atomic.Int64
	slots    chan struct{}
}

type boundedTransport struct {
	base http.RoundTripper
	opts Options
}

func (t boundedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	budget, ok := request.Context().Value(budgetKey{}).(*readBudget)
	if !ok {
		return nil, errMissingBudget
	}
	if budget.requests.Add(1) > int64(t.opts.MaxRequests) {
		return nil, errRequestLimit
	}
	select {
	case budget.slots <- struct{}{}:
	case <-request.Context().Done():
		return nil, request.Context().Err()
	}
	release := func() { <-budget.slots }
	response, err := t.base.RoundTrip(request)
	if err != nil {
		release()
		return nil, err
	}
	if response.ContentLength > t.opts.MaxResponseBytes {
		response.Body.Close()
		release()
		return nil, errResponseLimit
	}
	response.Body = &boundedBody{body: response.Body, budget: budget, perResponse: t.opts.MaxResponseBytes, total: t.opts.MaxTotalBytes, release: release}
	return response, nil
}

type boundedBody struct {
	body                     io.ReadCloser
	budget                   *readBudget
	perResponse, total, read int64
	release                  func()
	once                     sync.Once
}

func (b *boundedBody) Read(buffer []byte) (int, error) {
	remaining := min(b.perResponse-b.read, b.total-b.budget.bytes.Load())
	if remaining < 0 {
		return 0, errTotalLimit
	}
	if int64(len(buffer)) > remaining+1 {
		buffer = buffer[:remaining+1]
	}
	n, err := b.body.Read(buffer)
	b.read += int64(n)
	total := b.budget.bytes.Add(int64(n))
	if b.read > b.perResponse {
		return n, errResponseLimit
	}
	if total > b.total {
		return n, errTotalLimit
	}
	return n, err
}

func (b *boundedBody) Close() error {
	err := b.body.Close()
	b.once.Do(b.release)
	return err
}

func withReadBudget(ctx context.Context) context.Context {
	return context.WithValue(ctx, budgetKey{}, &readBudget{slots: make(chan struct{}, 4)})
}
