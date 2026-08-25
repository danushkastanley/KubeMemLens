package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/certbootstrap"
	"github.com/danushkastanley/kube-memlens/internal/kube"
)

func main() {
	namespace := flag.String("namespace", "kube-memlens", "KubeMemLens namespace")
	secret := flag.String("secret", "kube-memlens-extension-tls", "serving TLS Secret name")
	apiService := flag.String("api-service", "v1alpha1.memory.kubememlens.io", "APIService name")
	service := flag.String("service", "kube-memlens-collector", "extension Service name")
	validity := flag.Duration("validity", 365*24*time.Hour, "new certificate validity")
	rotateBefore := flag.Duration("rotate-before", 30*24*time.Hour, "rotate when less validity remains")
	force := flag.Bool("force", false, "rotate even when the current certificate remains valid")
	flag.Parse()

	config, err := kube.BuildConfig("", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "certificate bootstrap requires Kubernetes access: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := certbootstrap.Run(ctx, config, certbootstrap.Options{
		Namespace: *namespace, SecretName: *secret, APIService: *apiService, ServiceName: *service,
		Validity: *validity, RotateBefore: *rotateBefore, ForceRotation: *force,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "certificate bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("KubeMemLens extension certificate is current")
}
