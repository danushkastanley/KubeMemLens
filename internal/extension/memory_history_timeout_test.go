package extension

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
)

var historyTimeoutPaths = map[string]string{
	"trends":         "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api/trends?source=prometheus",
	"trends-context": contextPath,
}

// The API server cancels a read at the request timeout and answers for the
// handler. A history read that gives up at the same moment races that answer,
// and the caller gets a generic 504 or a dropped connection. Here the history
// source never answers, and the request has already spent time before it
// reaches the handler, as it does behind authentication and authorisation.
func TestStalledHistoryReadAnswersBeforeTheServerTimesOut(t *testing.T) {
	const requestTimeout, spentBeforeHandler = historyReadMargin + 500*time.Millisecond, 200 * time.Millisecond
	for name, path := range historyTimeoutPaths {
		t.Run(name, func(t *testing.T) {
			h, _ := contextReadFixture(t)
			h.memoryHistory.remote = historyProviderFunc(func(ctx context.Context, _ memoryhistory.Selection, _ memoryhistory.Query) (memoryhistory.Report, error) {
				<-ctx.Done()
				return memoryhistory.Report{}, ctx.Err()
			})
			check := genericfilters.BasicLongRunningRequestCheck(sets.NewString("watch"), sets.NewString())
			delayed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(spentBeforeHandler)
				h.ServeHTTP(w, r)
			})
			chain := genericfilters.WithTimeoutForNonLongRunningRequests(delayed, check)
			chain = genericapifilters.WithRequestDeadline(chain, nil, nil, check, serializer.NewCodecFactory(runtime.NewScheme()), requestTimeout)
			chain = genericapifilters.WithRequestReceivedTimestamp(chain)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authenticated := readRequest(t, r.URL.RequestURI(), true).Context()
				info, _ := apirequest.RequestInfoFrom(authenticated)
				user, _ := apirequest.UserFrom(authenticated)
				chain.ServeHTTP(w, r.WithContext(apirequest.WithUser(apirequest.WithRequestInfo(r.Context(), info), user)))
			}))
			defer server.Close()

			response, err := http.Get(server.URL + path)
			if err != nil {
				t.Fatalf("the caller got no answer: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want the handler's own 503", response.StatusCode)
			}
		})
	}
}

// Outside the server's filters a request carries no deadline; the read must
// still be bounded.
func TestHistoryReadWithoutARequestDeadlineIsStillBounded(t *testing.T) {
	for name, path := range historyTimeoutPaths {
		t.Run(name, func(t *testing.T) {
			h, _ := contextReadFixture(t)
			var budget time.Duration
			h.memoryHistory.remote = historyProviderFunc(func(ctx context.Context, _ memoryhistory.Selection, _ memoryhistory.Query) (memoryhistory.Report, error) {
				if deadline, ok := ctx.Deadline(); ok {
					budget = time.Until(deadline)
				}
				return memoryhistory.Report{}, context.DeadlineExceeded
			})
			if code := serveRead(t, h, path).Code; code != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want the handler's own 503", code)
			}
			if budget <= 0 || budget > RequestTimeout-historyReadMargin {
				t.Fatalf("history read budget %s, want at most %s", budget.Round(time.Second), RequestTimeout-historyReadMargin)
			}
		})
	}
}
