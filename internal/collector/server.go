package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

type HandlerOptions struct {
	SnapshotTTL      time.Duration
	MaxSnapshotBytes int64
	MaxContainers    int
	MaxSnapshotAge   time.Duration
	MaxFutureSkew    time.Duration
	MaxResponseBytes int
	Metrics          metrics.Options
}

const (
	defaultMaxSnapshotBytes int64 = 4 << 20
	defaultMaxContainers          = 10_000
	defaultMaxSnapshotAge         = 2 * time.Minute
	defaultMaxFutureSkew          = 30 * time.Second
	defaultMaxResponseBytes       = 16 << 20
)

func NewHandler(store *Store, ttl time.Duration, logf func(string, ...any)) http.Handler {
	return NewHandlerWithOptions(store, DefaultHandlerOptions(ttl), logf)
}

func DefaultHandlerOptions(ttl time.Duration) HandlerOptions {
	return HandlerOptions{
		SnapshotTTL:      ttl,
		MaxSnapshotBytes: defaultMaxSnapshotBytes,
		MaxContainers:    defaultMaxContainers,
		MaxSnapshotAge:   defaultMaxSnapshotAge,
		MaxFutureSkew:    defaultMaxFutureSkew,
		MaxResponseBytes: defaultMaxResponseBytes,
		Metrics:          metrics.DefaultOptions(),
	}
}

func NewHandlerWithOptions(store *Store, opts HandlerOptions, logf func(string, ...any)) http.Handler {
	opts = defaultHandlerOptions(opts)
	mux := http.NewServeMux()
	registerHealth(mux)
	registerIngestion(mux, store, opts, logf)
	registerReads(mux, store, opts)
	return mux
}

func NewReadHandlerWithOptions(store *Store, opts HandlerOptions) http.Handler {
	opts = defaultHandlerOptions(opts)
	mux := http.NewServeMux()
	registerHealth(mux)
	registerReads(mux, store, opts)
	return mux
}

func NewIngestHandlerWithOptions(store *Store, opts HandlerOptions, logf func(string, ...any)) http.Handler {
	opts = defaultHandlerOptions(opts)
	mux := http.NewServeMux()
	registerHealth(mux)
	registerIngestion(mux, store, opts, logf)
	return mux
}

func registerHealth(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}))
}

func registerIngestion(mux *http.ServeMux, store *Store, opts HandlerOptions, logf func(string, ...any)) {
	mux.HandleFunc("/api/v1/snapshots", method(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		result := "store_error"
		defer func() {
			store.RecordIngestion(result, time.Since(startedAt))
		}()
		if err := requireJSON(r); err != nil {
			result = "unsupported_media_type"
			writeError(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		snapshot, err := decodeSnapshot(w, r, opts.MaxSnapshotBytes)
		if err != nil {
			var maxBytesError *http.MaxBytesError
			if errors.As(err, &maxBytesError) {
				result = "payload_too_large"
				writeError(w, http.StatusRequestEntityTooLarge, "snapshot exceeds maximum request size")
				return
			}
			result = "invalid_json"
			writeError(w, http.StatusBadRequest, "invalid snapshot JSON")
			return
		}
		if err := validateSnapshot(snapshot, time.Now().UTC(), opts); err != nil {
			result = "invalid_snapshot"
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		count, err := store.ReplaceNodeSnapshot(snapshot)
		if errors.Is(err, ErrSnapshotOutOfOrder) {
			result = "out_of_order"
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, ErrStoreCapacity) {
			result = "store_capacity"
			writeError(w, http.StatusInsufficientStorage, err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "store snapshot")
			return
		}
		result = "accepted"
		logf("snapshot stored node=%s containers=%d", snapshot.NodeName, count)
		writeJSON(w, http.StatusOK, api.SnapshotPostResponse{OK: true, Containers: count})
	}))
}

func registerReads(mux *http.ServeMux, store *Store, opts HandlerOptions) {
	mux.HandleFunc("/api/v1/pages/containers", method(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		writeContainerPage(w, r, store, opts)
	}))
	mux.HandleFunc("/api/v1/containers", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeBoundedJSON(w, store.ListContainers(time.Now().UTC(), opts.SnapshotTTL), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/pods", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeBoundedJSON(w, store.ListPods(time.Now().UTC(), opts.SnapshotTTL), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/namespaces", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeBoundedJSON(w, store.ListNamespaces(time.Now().UTC(), opts.SnapshotTTL), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/nodes", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeBoundedJSON(w, store.ListNodes(time.Now().UTC(), opts.SnapshotTTL), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/workloads", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeBoundedJSON(w, store.ListWorkloads(time.Now().UTC(), opts.SnapshotTTL), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/history/pods/", method(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/history/pods/"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(parts[0]) > 253 || len(parts[1]) > 253 {
			writeError(w, http.StatusBadRequest, "history path must be /api/v1/history/pods/{namespace}/{pod}")
			return
		}
		writeBoundedJSON(w, store.ListPodHistory(parts[0], parts[1], "", time.Now().UTC()), opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/api/v1/debug/store", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		debug := store.Debug(time.Now().UTC(), opts.SnapshotTTL)
		debug.MaxResponseBytes = opts.MaxResponseBytes
		writeBoundedJSON(w, debug, opts.MaxResponseBytes)
	}))
	mux.HandleFunc("/metrics", metricsHandler(store, opts))
}

func defaultHandlerOptions(opts HandlerOptions) HandlerOptions {
	if opts.MaxSnapshotBytes <= 0 {
		opts.MaxSnapshotBytes = defaultMaxSnapshotBytes
	}
	if opts.MaxContainers <= 0 {
		opts.MaxContainers = defaultMaxContainers
	}
	if opts.MaxSnapshotAge <= 0 {
		opts.MaxSnapshotAge = defaultMaxSnapshotAge
	}
	if opts.MaxFutureSkew <= 0 {
		opts.MaxFutureSkew = defaultMaxFutureSkew
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = defaultMaxResponseBytes
	}
	if opts.Metrics == (metrics.Options{}) {
		opts.Metrics = metrics.DefaultOptions()
	}
	return opts
}

func requireJSON(r *http.Request) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return fmt.Errorf("Content-Type must be application/json")
	}
	return nil
}

func decodeSnapshot(w http.ResponseWriter, r *http.Request, maxBytes int64) (api.AgentSnapshot, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var snapshot api.AgentSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return api.AgentSnapshot{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return api.AgentSnapshot{}, fmt.Errorf("snapshot must contain one JSON object")
	}
	return snapshot, nil
}

func metricsHandler(store *Store, opts HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if !opts.Metrics.Enabled {
			http.Error(w, "metrics are disabled", http.StatusNotFound)
			return
		}

		body, err := (metrics.Exporter{
			Source: store,
			TTL:    opts.SnapshotTTL,
			Opts:   opts.Metrics,
		}).Render()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", metrics.ContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
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

func validateSnapshot(snapshot api.AgentSnapshot, now time.Time, opts HandlerOptions) error {
	if snapshot.SchemaVersion != api.CurrentSnapshotSchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d; expected %d", snapshot.SchemaVersion, api.CurrentSnapshotSchemaVersion)
	}
	if strings.TrimSpace(snapshot.NodeName) == "" {
		return fmt.Errorf("nodeName is required")
	}
	if len(snapshot.NodeName) > 253 {
		return fmt.Errorf("nodeName exceeds 253 bytes")
	}
	if snapshot.CapturedAt.IsZero() {
		return fmt.Errorf("capturedAt is required")
	}
	if snapshot.CapturedAt.Before(now.Add(-opts.MaxSnapshotAge)) {
		return fmt.Errorf("capturedAt is older than the accepted snapshot window")
	}
	if snapshot.CapturedAt.After(now.Add(opts.MaxFutureSkew)) {
		return fmt.Errorf("capturedAt is too far in the future")
	}
	if len(snapshot.Containers) > opts.MaxContainers {
		return fmt.Errorf("containers exceeds maximum of %d", opts.MaxContainers)
	}
	if snapshot.Environment.CgroupReadErrors < 0 {
		return fmt.Errorf("environment.cgroupReadErrors must not be negative")
	}
	if snapshot.Environment.WorkloadContextErrors < 0 {
		return fmt.Errorf("environment.workloadContextErrors must not be negative")
	}
	if len(snapshot.Environment.CgroupVersion) > 32 || len(snapshot.Environment.CgroupDriver) > 32 {
		return fmt.Errorf("environment cgroup fields exceed 32 bytes")
	}
	if len(snapshot.Environment.MemoryPressureStatus) > 16 {
		return fmt.Errorf("environment.memoryPressureStatus exceeds 16 bytes")
	}
	if len(snapshot.Environment.ContainerRuntimes) > 8 {
		return fmt.Errorf("environment.containerRuntimes exceeds maximum of 8")
	}
	for i, runtime := range snapshot.Environment.ContainerRuntimes {
		if len(runtime) > 32 {
			return fmt.Errorf("environment.containerRuntimes[%d] exceeds 32 bytes", i)
		}
	}
	containerIDs := make(map[string]struct{}, len(snapshot.Containers))
	for i, container := range snapshot.Containers {
		if strings.TrimSpace(container.ContainerID) == "" {
			return fmt.Errorf("containers[%d].containerID is required", i)
		}
		if len(container.ContainerID) > 256 {
			return fmt.Errorf("containers[%d].containerID exceeds 256 bytes", i)
		}
		if _, exists := containerIDs[container.ContainerID]; exists {
			return fmt.Errorf("containers[%d].containerID is duplicated", i)
		}
		containerIDs[container.ContainerID] = struct{}{}
		if len(container.CgroupPath) > 4096 {
			return fmt.Errorf("containers[%d].cgroupPath exceeds 4096 bytes", i)
		}
		if err := validateContainerContext(container.Context); err != nil {
			return fmt.Errorf("containers[%d].context: %w", i, err)
		}
		if container.NodeName != "" && container.NodeName != snapshot.NodeName {
			return fmt.Errorf("containers[%d].nodeName does not match snapshot nodeName", i)
		}
	}
	return nil
}

func validateContainerContext(context api.ContainerContext) error {
	if context.RestartCount < 0 {
		return fmt.Errorf("restartCount must not be negative")
	}
	for name, value := range map[string]string{
		"qosClass":              context.QoSClass,
		"lastTerminationReason": context.LastTerminationReason,
		"podPhase":              context.PodPhase,
		"ownerKind":             context.OwnerKind,
		"workloadKind":          context.WorkloadKind,
		"runtimeClassName":      context.RuntimeClassName,
	} {
		if len(value) > 64 {
			return fmt.Errorf("%s exceeds 64 bytes", name)
		}
	}
	if len(context.OwnerName) > 253 {
		return fmt.Errorf("ownerName exceeds 253 bytes")
	}
	if len(context.WorkloadName) > 253 {
		return fmt.Errorf("workloadName exceeds 253 bytes")
	}
	if context.MemoryEmptyDirCount < 0 || context.MemoryEmptyDirCount > 256 || context.MemoryEmptyDirLimited < 0 || context.MemoryEmptyDirLimited > context.MemoryEmptyDirCount {
		return fmt.Errorf("memory emptyDir counts are invalid")
	}
	if len(context.Labels) > 64 {
		return fmt.Errorf("labels exceeds maximum of 64")
	}
	for key, value := range context.Labels {
		if len(key) > 253 || len(value) > 253 {
			return fmt.Errorf("label key or value exceeds 253 bytes")
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "encode JSON", http.StatusInternalServerError)
		return
	}
	body = append(body, '\n')
	writeJSONBody(w, status, body)
}

func writeBoundedJSON(w http.ResponseWriter, value any, maxBytes int) {
	body, err := encodeBoundedJSON(value, maxBytes)
	if err != nil {
		writeError(w, http.StatusInsufficientStorage, "response exceeds configured maximum size")
		return
	}
	writeJSONBody(w, http.StatusOK, body)
}

func encodeBoundedJSON(value any, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxResponseBytes
	}
	buffer := bytes.NewBuffer(make([]byte, 0, min(maxBytes, 64<<10)))
	limited := &boundedBuffer{buffer: buffer, remaining: maxBytes}
	if err := json.NewEncoder(limited).Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeJSONBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

type boundedBuffer struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, fmt.Errorf("response limit exceeded")
	}
	w.remaining -= len(data)
	return w.buffer.Write(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
