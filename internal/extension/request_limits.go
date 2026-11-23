package extension

import (
	"net/http"

	"k8s.io/apimachinery/pkg/util/sets"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

// outsideRequestLimits keeps health probes out of the in-flight request limits.
// The kubelet's readiness probe is the signal that keeps this server behind its
// APIService; if it competed with reads for a slot, a burst of reads would get
// the probe refused and take the API away from every reader.
//
// The server library exempts exactly the requests this check accepts, and it
// also leaves them without a request timeout. Health handlers only read state
// that background loops maintain, and the kubelet applies its own timeout.
func outsideRequestLimits(next apirequest.LongRunningRequestCheck) apirequest.LongRunningRequestCheck {
	health := sets.New(delegatedAlwaysAllowPaths()...)
	return func(r *http.Request, info *apirequest.RequestInfo) bool {
		return next(r, info) || !info.IsResourceRequest && r.Method == http.MethodGet && health.Has(info.Path)
	}
}
