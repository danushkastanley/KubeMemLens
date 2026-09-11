package agentless

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRefreshTransportBoundsCombinedBytesAndRequests(t *testing.T) {
	ctx := withReadBudget(t.Context())
	opts := Options{MaxResponseBytes: 16, MaxTotalBytes: 8, MaxRequests: 2}
	calls := 0
	transport := boundedTransport{opts: opts, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, ContentLength: 6, Body: io.NopCloser(strings.NewReader("123456"))}, nil
	})}
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://example.invalid", nil)
	first, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(first.Body); err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	second, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(second.Body); !errors.Is(err, errTotalLimit) {
		t.Fatalf("error=%v", err)
	}
	second.Body.Close()
	if _, err = transport.RoundTrip(request); !errors.Is(err, errRequestLimit) || calls != 2 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	if slots := len(ctx.Value(budgetKey{}).(*readBudget).slots); slots != 0 {
		t.Fatalf("slots leaked: %d", slots)
	}
}

func TestRefreshTransportBoundsUnknownLengthAndCancelsSlotWait(t *testing.T) {
	ctx := withReadBudget(t.Context())
	transport := boundedTransport{opts: Options{MaxResponseBytes: 4, MaxTotalBytes: 32, MaxRequests: 8}, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader("123456"))}, nil
	})}
	request, _ := http.NewRequestWithContext(ctx, "GET", "https://example.invalid", nil)
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(response.Body); !errors.Is(err, errResponseLimit) {
		t.Fatalf("error=%v", err)
	}
	response.Body.Close()
	budget := ctx.Value(budgetKey{}).(*readBudget)
	for i := 0; i < cap(budget.slots); i++ {
		budget.slots <- struct{}{}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = transport.RoundTrip(request.WithContext(cancelled)); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestKubernetesReadsUseBoundsAndRejectRedirects(t *testing.T) {
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	reader, err := NewNamespace(&rest.Config{Host: server.URL}, "team-a", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.client.CoreV1().Pods("team-a").List(withReadBudget(t.Context()), metav1.ListOptions{}); err == nil || redirected {
		t.Fatalf("redirected=%v err=%v", redirected, err)
	}
	if _, err = reader.client.CoreV1().Pods("team-a").List(t.Context(), metav1.ListOptions{}); !errors.Is(err, errMissingBudget) {
		t.Fatalf("unbounded request allowed: %v", err)
	}
}

func TestReaderConfigurationRejectsAmbiguousScopeAndExcessiveBounds(t *testing.T) {
	config := &rest.Config{Host: "https://example.invalid"}
	for _, opts := range []Options{{MaxTotalBytes: 1 << 40}, {MaxOwnerReads: -1}, {MaxRequests: 5001}} {
		if _, err := NewCluster(config, opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
	for _, namespace := range []string{"", "../other"} {
		if _, err := NewNamespace(config, namespace, Options{}); err == nil {
			t.Fatalf("accepted %q", namespace)
		}
	}
}
