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
	"strings"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/agent"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	ingestionLegacy        = "legacy"
	ingestionAuthenticated = "authenticated"
)

func main() {
	cgroupRoot := flag.String("cgroup-root", "/sys/fs/cgroup", "cgroup v2 root to inspect")
	interval := flag.Duration("interval", 5*time.Second, "scan interval")
	once := flag.Bool("once", false, "scan the configured cgroup root once")
	collectorURL := flag.String("collector-url", "", "collector base URL")
	ingestionMode := flag.String("ingestion-mode", ingestionLegacy, "snapshot ingestion mode: legacy or authenticated")
	nodeName := flag.String("node-name", defaultNodeName(), "Kubernetes node name")
	kubeEnabled := flag.Bool("kube", true, "use Kubernetes API for pod/container metadata")
	scanTimeout := flag.Duration("scan-timeout", 10*time.Second, "per-scan timeout")
	publishTimeout := flag.Duration("publish-timeout", 10*time.Second, "maximum time for one authenticated snapshot publish including bounded retries")
	cacheSyncTimeout := flag.Duration("cache-sync-timeout", 15*time.Second, "maximum initial Kubernetes metadata cache sync wait")
	metricsListenAddr := flag.String("metrics-listen", "127.0.0.1:8082", "HTTP listen address for agent metrics and health; empty disables it; use an explicit non-loopback address only in a reviewed local environment")
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
	if *interval <= 0 || *scanTimeout <= 0 || *publishTimeout <= 0 || *cacheSyncTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "interval and operation timeouts must be greater than zero")
		os.Exit(2)
	}

	var kubeClient kubernetes.Interface
	var kubeConfig *rest.Config
	if *kubeEnabled {
		config, err := kube.BuildConfig("", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "kubernetes metadata mapping unavailable: %v\n", err)
		} else {
			config.UserAgent = "kube-memlens-agent/" + buildinfo.Version
			client, clientErr := kube.NewClientForConfig(config)
			if clientErr != nil {
				fmt.Fprintf(os.Stderr, "kubernetes metadata mapping unavailable: %v\n", clientErr)
			} else {
				kubeConfig = config
				kubeClient = client
			}
		}
	}
	var publisher *agent.SnapshotPublisher
	if *ingestionMode == ingestionAuthenticated {
		if kubeConfig == nil {
			fmt.Fprintln(os.Stderr, "authenticated ingestion requires a working Kubernetes client")
			os.Exit(1)
		}
		var err error
		publisher, err = agent.NewSnapshotPublisher(kubeConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "authenticated ingestion unavailable: %v\n", err)
			os.Exit(1)
		}
	}

	scanner := agent.Scanner{
		CgroupRoot: *cgroupRoot,
		NodeName:   *nodeName,
		Kube:       *kubeEnabled,
	}
	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), *scanTimeout)

		index, err := loadPodIndex(ctx, kubeClient, *nodeName, *kubeEnabled)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load Kubernetes metadata: %v\n", err)
			os.Exit(1)
		}
		result, err := scanner.Scan(ctx, index)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan failed: reason=%s\n", boundedScanFailureReason(err))
			os.Exit(1)
		}

		fmt.Printf("Scanned %s\n", *cgroupRoot)
		fmt.Printf("Container cgroups found: %d\n", result.ContainersFound)
		fmt.Printf("Mapped containers: %d\n", result.Mapped)
		fmt.Printf("Unmapped containers: %d\n", result.Unmapped)
		fmt.Printf("Infrastructure cgroups: %d\n", result.InfrastructureCgroups)
		fmt.Printf("Total charged memory: %s\n", model.FormatBytes(result.TotalMemory.TotalBytes))
		if result.RootFallback {
			fmt.Println("Used direct root cgroup scan because no container cgroups were found.")
		}
		if *collectorURL != "" || publisher != nil {
			if reason := snapshotPublishBlockReason(true, result); reason != "" {
				fmt.Fprintf(os.Stderr, "collector POST skipped: reason=%s\n", reason)
				os.Exit(1)
			}
			publishCtx, publishCancel := context.WithTimeout(context.Background(), *publishTimeout)
			err := publishSnapshot(publishCtx, *ingestionMode, *collectorURL, publisher, index.NodeContext.NodeUID, result.Snapshot)
			publishCancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "collector POST failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Collector POST: ok")
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	telemetry := &agent.Telemetry{}
	var metricsServer *http.Server
	if strings.TrimSpace(*metricsListenAddr) != "" {
		metricsServer = newMetricsServer(*metricsListenAddr, telemetry.Handler())
		go func() {
			fmt.Printf("memlens-agent metrics endpoint listening on %s\n", metricsServer.Addr)
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "memlens-agent metrics server failed: %v\n", err)
				stop()
			}
		}()
		defer shutdownMetricsServer(metricsServer)
	}
	var podCache *kube.PodCache
	var nodeContextCache *kube.NodeContextCache
	var ownerResolver *kube.WorkloadOwnerResolver
	if *kubeEnabled && kubeClient != nil {
		podCache = kube.NewPodCache(kubeClient, *nodeName)
		go podCache.Run(ctx)
		syncCtx, syncCancel := context.WithTimeout(ctx, *cacheSyncTimeout)
		if podCache.WaitForSync(syncCtx) {
			fmt.Printf("kubernetes metadata cache synced node=%s pods=%d\n", *nodeName, podCache.PodCount())
		} else {
			fmt.Fprintf(os.Stderr, "kubernetes metadata cache initial sync timed out node=%s\n", *nodeName)
		}
		syncCancel()
		nodeContextCache = kube.NewNodeContextCache(kubeClient, *nodeName, 30*time.Second)
		ownerResolver = kube.NewWorkloadOwnerResolver(kubeClient, 5*time.Minute)
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	nodeContextErrorReported := false
	workloadContextErrorReported := false
	for {
		start := time.Now()
		scanCtx, cancel := context.WithTimeout(ctx, *scanTimeout)
		index := kube.EmptyPodIndex()
		metadataCachePods := 0
		metadataSynced := podCache == nil || podCache.Synced()
		if podCache != nil && metadataSynced {
			index = podCache.Index()
			metadataCachePods = podCache.PodCount()
		}
		if ownerResolver != nil {
			var resolutionErrors int
			index, resolutionErrors = ownerResolver.ResolveIndex(scanCtx, index, time.Now().UTC())
			if resolutionErrors > 0 && !workloadContextErrorReported {
				fmt.Fprintf(os.Stderr, "Kubernetes workload owner context unavailable for %d containers\n", resolutionErrors)
				workloadContextErrorReported = true
			} else if resolutionErrors == 0 {
				workloadContextErrorReported = false
			}
		}
		if nodeContextCache != nil {
			nodeContext, nodeErr := nodeContextCache.Context(scanCtx, time.Now().UTC())
			index.NodeContext = nodeContext
			if nodeErr != nil && !nodeContextErrorReported {
				fmt.Fprintf(os.Stderr, "Kubernetes node context unavailable: %v\n", nodeErr)
				nodeContextErrorReported = true
			} else if nodeErr == nil {
				nodeContextErrorReported = false
			}
		}
		result, scanErr := scanner.Scan(scanCtx, index)
		cancel()
		telemetry.RecordScan(time.Now().UTC(), time.Since(start), result, scanErr, metadataCachePods)
		failureReason := agentFailureReason("")
		if scanErr != nil {
			failureReason = boundedScanFailureReason(scanErr)
		}
		posted := false
		if scanErr == nil && (*collectorURL != "" || publisher != nil) {
			if blockReason := snapshotPublishBlockReason(metadataSynced, result); blockReason != "" {
				failureReason = blockReason
			} else {
				publishCtx, publishCancel := context.WithTimeout(ctx, *publishTimeout)
				postErr := publishSnapshot(publishCtx, *ingestionMode, *collectorURL, publisher, index.NodeContext.NodeUID, result.Snapshot)
				publishCancel()
				if postErr != nil {
					telemetry.RecordPost(postErr)
					failureReason = agentFailureSnapshotPost
				} else {
					telemetry.RecordPost(nil)
					posted = true
				}
			}
		}

		if failureReason != "" {
			fmt.Print(formatAgentFailure(*nodeName, failureReason, time.Since(start)))
		} else {
			if result.WalkError != nil {
				fmt.Print(formatCgroupReadWarning(*nodeName, result.Snapshot.Environment.CgroupReadErrors))
			}
			fmt.Printf(
				"scan complete node=%s containers=%d mapped=%d unmapped=%d infrastructure=%d posted=%t duration=%s\n",
				*nodeName,
				result.ContainersFound,
				result.Mapped,
				result.Unmapped,
				result.InfrastructureCgroups,
				posted,
				time.Since(start).Round(time.Millisecond),
			)
		}

		select {
		case <-ctx.Done():
			fmt.Println("memlens-agent shutting down")
			return
		case <-ticker.C:
		}
	}
}

func publishSnapshot(ctx context.Context, mode, collectorURL string, publisher *agent.SnapshotPublisher, nodeUID string, snapshot api.AgentSnapshot) error {
	if mode == ingestionAuthenticated {
		return publisher.Publish(ctx, nodeUID, snapshot)
	}
	return agent.PostSnapshot(ctx, collectorURL, snapshot)
}

func newMetricsServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func shutdownMetricsServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "memlens-agent metrics shutdown failed: %v\n", err)
	}
}

func loadPodIndex(ctx context.Context, client kubernetes.Interface, nodeName string, kubeEnabled bool) (kube.PodIndex, error) {
	index := kube.EmptyPodIndex()
	if kubeEnabled && client != nil {
		pods, err := kube.ListPodsForNode(ctx, client, nodeName)
		if err != nil {
			return kube.PodIndex{}, err
		}
		index = kube.BuildPodIndexFromPods(pods)
		var resolutionErrors int
		index, resolutionErrors = kube.NewWorkloadOwnerResolver(client, 5*time.Minute).ResolveIndex(ctx, index, time.Now().UTC())
		if resolutionErrors > 0 {
			return kube.PodIndex{}, fmt.Errorf("resolve %d Kubernetes workload owners", resolutionErrors)
		}
		nodeContext, err := kube.NewNodeContextCache(client, nodeName, time.Minute).Context(ctx, time.Now().UTC())
		if err != nil {
			return kube.PodIndex{}, err
		}
		index.NodeContext = nodeContext
	}
	return index, nil
}

func defaultNodeName() string {
	if value := os.Getenv("NODE_NAME"); value != "" {
		return value
	}
	hostname, err := os.Hostname()
	if err == nil {
		return hostname
	}
	return ""
}
