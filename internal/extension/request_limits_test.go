package extension

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
)

func TestHealthProbesAreServedWhileEveryReadSlotIsHeld(t *testing.T) {
	const readPath = "/apis/memory.kubememlens.io/v1alpha1/containers"
	entered, release := make(chan struct{}), make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == readPath {
			entered <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusOK)
	})
	check := outsideRequestLimits(genericfilters.BasicLongRunningRequestCheck(sets.NewString("watch"), sets.NewString()))
	limited := genericfilters.WithMaxInFlightLimit(inner, 1, 1, check)
	server := genericapifilters.WithRequestInfo(limited, &apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("apis"), GrouplessAPIPrefixes: sets.NewString()})
	status := func(method, path string) int {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder.Code
	}

	held := make(chan int)
	go func() { held <- status(http.MethodGet, readPath) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first read never reached the handler")
	}
	defer func() {
		close(release)
		if code := <-held; code != http.StatusOK {
			t.Errorf("held read finished with %d", code)
		}
	}()

	for _, path := range delegatedAlwaysAllowPaths() {
		if code := status(http.MethodGet, path); code != http.StatusOK {
			t.Errorf("GET %s with the read slot held: status %d, want 200", path, code)
		}
	}
	// The exemption is for probes only: reads and other methods still queue for a slot.
	for _, request := range [][2]string{{http.MethodGet, readPath}, {http.MethodGet, "/readyz/extra"}, {http.MethodGet, "/apis/memory.kubememlens.io/v1alpha1/readyz"}} {
		if code := status(request[0], request[1]); code != http.StatusTooManyRequests {
			t.Errorf("%s %s with the read slot held: status %d, want 429", request[0], request[1], code)
		}
	}
}
