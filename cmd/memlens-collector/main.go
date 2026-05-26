package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/collector"
)

func main() {
	listenAddr := flag.String("listen", ":8080", "HTTP listen address for collector health checks")
	snapshotTTL := flag.Duration("snapshot-ttl", 30*time.Second, "duration before snapshots are hidden from query responses")
	flag.Parse()

	store := collector.NewStore()
	fmt.Println("memlens-collector started with in-memory latest snapshot store")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              *listenAddr,
		Handler:           collector.NewHandler(store, *snapshotTTL, func(format string, args ...any) { fmt.Printf(format+"\n", args...) }),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		fmt.Printf("memlens-collector health endpoint listening on %s\n", *listenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("memlens-collector server failed: %v\n", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Printf("memlens-collector shutdown failed: %v\n", err)
	}
	fmt.Println("memlens-collector shutting down")
}
