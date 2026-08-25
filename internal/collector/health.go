package collector

import "net/http"

// NewHealthHandler exposes only data-free process liveness. Readiness belongs
// to the authenticated extension server because it depends on Kubernetes
// request-header configuration and delegated authorisation connectivity.
func NewHealthHandler() http.Handler {
	mux := http.NewServeMux()
	registerHealth(mux)
	return mux
}
