package collector

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func NewHandler(store *Store, ttl time.Duration, logf func(string, ...any)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}))
	mux.HandleFunc("/api/v1/snapshots", method(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		var snapshot api.AgentSnapshot
		if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := validateSnapshot(snapshot); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		count := store.UpsertSnapshot(snapshot)
		logf("snapshot stored node=%s containers=%d", snapshot.NodeName, count)
		writeJSON(w, http.StatusOK, api.SnapshotPostResponse{OK: true, Containers: count})
	}))
	mux.HandleFunc("/api/v1/containers", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, store.ListContainers(time.Now().UTC(), ttl))
	}))
	mux.HandleFunc("/api/v1/pods", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, store.ListPods(time.Now().UTC(), ttl))
	}))
	mux.HandleFunc("/api/v1/namespaces", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, store.ListNamespaces(time.Now().UTC(), ttl))
	}))
	mux.HandleFunc("/api/v1/debug/store", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, store.Debug(time.Now().UTC(), ttl))
	}))
	return mux
}

func method(allowed string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed {
			w.Header().Set("Allow", allowed)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next(w, r)
	}
}

func validateSnapshot(snapshot api.AgentSnapshot) error {
	if snapshot.NodeName == "" {
		return fmt.Errorf("nodeName is required")
	}
	if snapshot.CapturedAt.IsZero() {
		return fmt.Errorf("capturedAt is required")
	}
	for i, container := range snapshot.Containers {
		if container.ContainerID == "" {
			return fmt.Errorf("containers[%d].containerID is required", i)
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
