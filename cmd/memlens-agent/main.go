package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/agent"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"k8s.io/client-go/kubernetes"
)

func main() {
	cgroupRoot := flag.String("cgroup-root", "/sys/fs/cgroup", "cgroup v2 root to inspect")
	interval := flag.Duration("interval", 5*time.Second, "scan interval")
	once := flag.Bool("once", false, "scan the configured cgroup root once")
	collectorURL := flag.String("collector-url", "", "collector base URL")
	nodeName := flag.String("node-name", defaultNodeName(), "Kubernetes node name")
	kubeEnabled := flag.Bool("kube", true, "use Kubernetes API for pod/container metadata")
	scanTimeout := flag.Duration("scan-timeout", 10*time.Second, "per-scan timeout")
	flag.Parse()

	var kubeClient kubernetes.Interface
	if *kubeEnabled {
		client, err := kube.NewClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "kubernetes metadata mapping unavailable: %v\n", err)
		} else {
			kubeClient = client
		}
	}

	scanner := agent.Scanner{
		CgroupRoot: *cgroupRoot,
		NodeName:   *nodeName,
		Kube:       *kubeEnabled,
	}
	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), *scanTimeout)
		defer cancel()

		result, err := runScan(ctx, scanner, kubeClient, *nodeName, *kubeEnabled)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Scanned %s\n", *cgroupRoot)
		fmt.Printf("Container cgroups found: %d\n", result.ContainersFound)
		fmt.Printf("Mapped containers: %d\n", result.Mapped)
		fmt.Printf("Unmapped containers: %d\n", result.Unmapped)
		fmt.Printf("Total charged memory: %s\n", model.FormatBytes(result.TotalMemory.TotalBytes))
		if result.RootFallback {
			fmt.Println("Used direct root cgroup scan because no container cgroups were found.")
		}
		if *collectorURL != "" {
			if err := agent.PostSnapshot(ctx, *collectorURL, result.Snapshot); err != nil {
				fmt.Fprintf(os.Stderr, "collector POST failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Collector POST: ok")
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		start := time.Now()
		scanCtx, cancel := context.WithTimeout(ctx, *scanTimeout)
		result, err := runScan(scanCtx, scanner, kubeClient, *nodeName, *kubeEnabled)
		posted := false
		if err == nil && *collectorURL != "" {
			if postErr := agent.PostSnapshot(scanCtx, *collectorURL, result.Snapshot); postErr != nil {
				err = postErr
			} else {
				posted = true
			}
		}
		cancel()

		if err != nil {
			fmt.Printf("scan failed node=%s error=%q duration=%s\n", *nodeName, err.Error(), time.Since(start).Round(time.Millisecond))
		} else {
			if result.WalkError != nil {
				fmt.Printf("scan warning node=%s error=%q\n", *nodeName, result.WalkError.Error())
			}
			fmt.Printf(
				"scan complete node=%s containers=%d mapped=%d unmapped=%d posted=%t duration=%s\n",
				*nodeName,
				result.ContainersFound,
				result.Mapped,
				result.Unmapped,
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

func runScan(ctx context.Context, scanner agent.Scanner, client kubernetes.Interface, nodeName string, kubeEnabled bool) (agent.ScanResult, error) {
	index := kube.PodIndex{
		ByPodUID:      map[string][]kube.PodRef{},
		ByContainerID: map[string]kube.PodRef{},
	}
	if kubeEnabled && client != nil {
		pods, err := kube.ListPodsForNode(ctx, client, nodeName)
		if err != nil {
			return agent.ScanResult{}, err
		}
		index = kube.BuildPodIndexFromPods(pods)
	}

	return scanner.Scan(ctx, index)
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
