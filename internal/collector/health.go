package collector

import "net/http"

// NewHealthHandler exposes only the data-free process health route used by
// kubelet probes in the authenticated profile.
func NewHealthHandler() http.Handler {
	mux := http.NewServeMux()
	registerHealth(mux)
	return mux
}
