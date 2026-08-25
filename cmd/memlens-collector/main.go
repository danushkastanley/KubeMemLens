package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/extension"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

const (
	ingestionLegacy        = "legacy"
	ingestionAuthenticated = "authenticated"
	collectorShutdownLimit = 25 * time.Second
	extensionShutdownDelay = 3 * time.Second
)

type serverResult struct {
	name string
	err  error
}

func main() {
	listenAddr := flag.String("listen", ":8080", "HTTP listen address for collector reads, metrics, and health checks")
	ingestListenAddr := flag.String("ingest-listen", ":8081", "HTTP listen address for agent snapshot ingestion")
	ingestionMode := flag.String("ingestion-mode", ingestionLegacy, "snapshot ingestion mode: legacy or authenticated")
	extensionPort := flag.Int("extension-port", 8443, "HTTPS port for the aggregated ingestion API")
	extensionCertFile := flag.String("extension-tls-cert-file", "", "aggregated API serving certificate")
	extensionKeyFile := flag.String("extension-tls-key-file", "", "aggregated API serving private key")
	extensionKubeconfig := flag.String("extension-kubeconfig", "", "optional kubeconfig for delegated authentication and authorisation")
	expectedNodeSelectorJSON := flag.String("expected-node-selector-json", `{"kubernetes.io/os":"linux"}`, "JSON Node selector matching the agent DaemonSet")
	expectedNodeTolerationsJSON := flag.String("expected-node-tolerations-json", `[]`, "JSON Node tolerations matching the agent DaemonSet")
	agentUsername := flag.String("agent-username", "system:serviceaccount:kube-memlens:kube-memlens-agent", "exact Kubernetes agent ServiceAccount username")
	ingestionMaxConcurrent := flag.Int("ingestion-max-concurrent", 4, "maximum snapshot bodies decoded concurrently")
	ingestionRequestsPerSecond := flag.Float64("ingestion-requests-per-second-per-agent", 1, "accepted request rate per authenticated agent Pod")
	ingestionBurst := flag.Int("ingestion-burst-per-agent", 2, "authenticated ingestion burst per agent Pod")
	readMaxConcurrent := flag.Int("read-max-concurrent", 4, "maximum authenticated read requests admitted concurrently")
	handlerOpts := collector.DefaultHandlerOptions(30 * time.Second)
	historyOpts := collector.DefaultHistoryOptions()
	storeLimits := collector.DefaultStoreLimits()
	flag.DurationVar(&handlerOpts.SnapshotTTL, "snapshot-ttl", handlerOpts.SnapshotTTL, "duration before last-known snapshots are marked stale")
	flag.Int64Var(&handlerOpts.MaxSnapshotBytes, "max-snapshot-bytes", handlerOpts.MaxSnapshotBytes, "maximum snapshot request size in bytes")
	flag.IntVar(&handlerOpts.MaxContainers, "max-snapshot-containers", handlerOpts.MaxContainers, "maximum containers accepted in one snapshot")
	flag.DurationVar(&handlerOpts.MaxSnapshotAge, "max-snapshot-age", handlerOpts.MaxSnapshotAge, "maximum accepted age of an incoming snapshot")
	flag.DurationVar(&handlerOpts.MaxFutureSkew, "max-snapshot-future-skew", handlerOpts.MaxFutureSkew, "maximum accepted future clock skew for a snapshot")
	flag.IntVar(&handlerOpts.MaxResponseBytes, "max-response-bytes", handlerOpts.MaxResponseBytes, "maximum encoded JSON bytes returned by one read request")
	flag.IntVar(&storeLimits.MaxNodes, "store-max-nodes", storeLimits.MaxNodes, "maximum node records retained in memory")
	flag.IntVar(&storeLimits.MaxContainers, "store-max-containers", storeLimits.MaxContainers, "maximum current container snapshots retained in memory")
	flag.DurationVar(&historyOpts.Duration, "history-duration", historyOpts.Duration, "maximum age of in-memory Pod history")
	flag.IntVar(&historyOpts.MaxSeries, "history-max-series", historyOpts.MaxSeries, "maximum Pod history series held in memory")
	flag.IntVar(&historyOpts.MaxPoints, "history-max-points", historyOpts.MaxPoints, "maximum points held per Pod history series")
	flag.IntVar(&historyOpts.MaxResponseSeries, "history-max-response-series", historyOpts.MaxResponseSeries, "maximum Pod instances returned by one history request")
	metricsOpts := metrics.DefaultOptions()
	flag.BoolVar(&metricsOpts.Enabled, "metrics-enabled", metricsOpts.Enabled, "enable Prometheus/OpenMetrics endpoint")
	flag.BoolVar(&metricsOpts.IncludeNamespaceMetrics, "metrics-include-namespaces", metricsOpts.IncludeNamespaceMetrics, "include namespace-level metrics")
	flag.BoolVar(&metricsOpts.IncludePodMetrics, "metrics-include-pods", metricsOpts.IncludePodMetrics, "include pod-level metrics")
	flag.BoolVar(&metricsOpts.IncludeContainerMetrics, "metrics-include-containers", metricsOpts.IncludeContainerMetrics, "include container-level metrics")
	flag.BoolVar(&metricsOpts.IncludeDiagnosisMetrics, "metrics-include-diagnosis", metricsOpts.IncludeDiagnosisMetrics, "include diagnosis metrics")
	flag.BoolVar(&metricsOpts.IncludeEventMetrics, "metrics-include-events", metricsOpts.IncludeEventMetrics, "include memory event metrics")
	flag.IntVar(&metricsOpts.MaxPods, "metrics-max-pods", metricsOpts.MaxPods, "maximum pod entities to expose before dropping pod metrics")
	flag.IntVar(&metricsOpts.MaxContainers, "metrics-max-containers", metricsOpts.MaxContainers, "maximum container entities to expose before dropping container metrics")
	versionOnly := flag.Bool("version", false, "show build information and exit")
	flag.Parse()
	if *versionOnly {
		fmt.Println(buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String())
		return
	}
	if *ingestionMode != ingestionLegacy && *ingestionMode != ingestionAuthenticated {
		fmt.Fprintln(os.Stderr, "ingestion mode must be legacy or authenticated")
		os.Exit(2)
	}
	if historyOpts.Duration <= 0 || historyOpts.MaxSeries <= 0 || historyOpts.MaxPoints <= 0 || historyOpts.MaxResponseSeries <= 0 || storeLimits.MaxNodes <= 0 || storeLimits.MaxContainers <= 0 || handlerOpts.MaxResponseBytes <= 0 || *readMaxConcurrent <= 0 {
		fmt.Fprintln(os.Stderr, "history, store, and response limits must be greater than zero")
		os.Exit(2)
	}
	handlerOpts.Metrics = metricsOpts
	historyOpts.ContinuityGap = handlerOpts.SnapshotTTL
	expectedNodeSelector, err := parseExpectedNodeSelector(*expectedNodeSelectorJSON)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	expectedNodeTolerations, err := parseExpectedNodeTolerations(*expectedNodeTolerationsJSON)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	store := collector.NewStoreWithHistoryAndLimits(historyOpts, storeLimits)
	fmt.Printf("memlens-collector started with bounded state maxNodes=%d maxContainers=%d maxResponseBytes=%d historyDuration=%s historyMaxSeries=%d historyMaxPoints=%d\n", storeLimits.MaxNodes, storeLimits.MaxContainers, handlerOpts.MaxResponseBytes, historyOpts.Duration, historyOpts.MaxSeries, historyOpts.MaxPoints)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serverResults := make(chan serverResult, 3)
	serverCount := 0

	readHandler := collector.NewHealthHandler()
	if *ingestionMode == ingestionLegacy {
		readHandler = collector.NewReadHandlerWithOptions(store, handlerOpts)
	}
	readServer := newHTTPServer(*listenAddr, readHandler)
	servers := []struct {
		name   string
		server *http.Server
	}{
		{name: "read", server: readServer},
	}
	if *ingestionMode == ingestionLegacy {
		ingestServer := newHTTPServer(*ingestListenAddr, collector.NewIngestHandlerWithOptions(store, handlerOpts, func(format string, args ...any) {
			fmt.Printf(format+"\n", args...)
		}))
		servers = append(servers, struct {
			name   string
			server *http.Server
		}{name: "ingest", server: ingestServer})
	} else {
		coordinator, err := extension.NewCoordinator(store, extension.CoordinatorOptions{
			Handler: handlerOpts, MaxAgents: storeLimits.MaxNodes, MaxRetired: storeLimits.MaxNodes * 4,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "configure authenticated ingestion: %v\n", err)
			os.Exit(1)
		}
		handler, err := extension.NewHandler(coordinator, extension.HandlerOptions{
			AgentUsername: *agentUsername, MaxSnapshotBytes: handlerOpts.MaxSnapshotBytes,
			MaxConcurrent: *ingestionMaxConcurrent, RequestsPerSec: *ingestionRequestsPerSecond,
			Burst: *ingestionBurst, MaxIdentities: storeLimits.MaxNodes,
			Logf: func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "configure authenticated ingestion: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("memlens-collector authenticated extension listening on :%d\n", *extensionPort)
		serverCount++
		startServer("extension", serverResults, stop, func() error {
			return (extension.ServerOptions{
				BindPort: *extensionPort, CertFile: *extensionCertFile, KeyFile: *extensionKeyFile,
				KubeconfigFile: *extensionKubeconfig, MaxBodyBytes: handlerOpts.MaxSnapshotBytes,
				MaxRead: *readMaxConcurrent, MaxMutating: 32, RequestTimeout: 10 * time.Second,
				ShutdownDelay: extensionShutdownDelay, NodeSelector: expectedNodeSelector,
				NodeTolerations: expectedNodeTolerations, Handler: handler,
			}).Run(ctx)
		})
	}

	for _, item := range servers {
		item := item
		fmt.Printf("memlens-collector %s endpoint listening on %s\n", item.name, item.server.Addr)
		serverCount++
		startServer(item.name, serverResults, stop, item.server.ListenAndServe)
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), collectorShutdownLimit)
	defer cancel()
	var shutdownErrors []error
	for _, item := range servers {
		if err := item.server.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("%s shutdown: %w", item.name, err))
		}
	}
	if err := waitForServerResults(shutdownCtx, serverResults, serverCount); err != nil {
		shutdownErrors = append(shutdownErrors, err)
	}
	fmt.Println("memlens-collector shutting down")
	if err := errors.Join(shutdownErrors...); err != nil {
		fmt.Fprintf(os.Stderr, "memlens-collector shutdown failed: %v\n", err)
		os.Exit(1)
	}
}

func startServer(name string, results chan<- serverResult, stop context.CancelFunc, run func() error) {
	go func() {
		err := normaliseServerError(run())
		results <- serverResult{name: name, err: err}
		if err != nil {
			fmt.Printf("memlens-collector %s server failed: %v\n", name, err)
			stop()
		}
	}()
}

func normaliseServerError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func waitForServerResults(ctx context.Context, results <-chan serverResult, count int) error {
	var failures []error
	for range count {
		select {
		case result := <-results:
			if result.err != nil {
				failures = append(failures, fmt.Errorf("%s server: %w", result.name, result.err))
			}
		case <-ctx.Done():
			failures = append(failures, fmt.Errorf("wait for server drain: %w", ctx.Err()))
			return errors.Join(failures...)
		}
	}
	return errors.Join(failures...)
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
