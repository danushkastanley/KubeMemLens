package kube

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestMarkerCancellationReleasesItsOnlyAdmissionSlot(t *testing.T) {
	f := newMarkerFixture(t)
	started := make(chan struct{})
	var once sync.Once
	f.edit(func() {
		f.beforeRead = func(r *http.Request) { once.Do(func() { close(started) }); <-r.Context().Done() }
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := f.provider.Query(ctx, f.selection, f.query); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("source request did not start")
	}
	if _, err := f.provider.Query(t.Context(), f.selection, f.query); !errors.Is(err, memoryhistory.ErrBounds) {
		t.Fatal("parallel marker query was not bounded", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled query did not stop")
	}
	f.edit(func() { f.beforeRead = nil })
	if _, err := f.provider.Query(t.Context(), f.selection, f.query); err != nil {
		t.Fatal("cancelled query retained the admission slot", err)
	}
}

func TestMarkerAcquisitionHonoursEarlierDeadline(t *testing.T) {
	f := newMarkerFixture(t)
	f.edit(func() { f.beforeRead = func(r *http.Request) { <-r.Context().Done() } })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := f.provider.Query(ctx, f.selection, f.query)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal("earlier deadline was not retained", err)
	}
}

func TestMarkerEventTransportRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, body := range []string{
		`{"apiVersion":"v1","kind":"EventList","items":[],"items":[{}]}`,
		`{"apiVersion":"v1","kind":"EventList","items":[]} {}`,
		`{"apiVersion":"v1","kind":"EventList","items":[` + strings.Repeat(`{},`, 258) + `{}]}`,
		`{"apiVersion":"v1","kind":"EventList","note":"` + strings.Repeat("a", maxHealthResponse) + `","items":[]}`,
	} {
		f := newMarkerFixture(t)
		f.eventRaw = []byte(body)
		if _, err := f.provider.Query(t.Context(), f.selection, f.query); err == nil {
			t.Fatal("hostile source body accepted")
		}
	}
}
