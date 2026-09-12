package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodestats"
)

func runWithMetrics(ctx context.Context, address string, telemetry *nodestats.Telemetry, run func(context.Context) error) error {
	if address == "" {
		return run(ctx)
	}
	endpoint, err := netip.ParseAddrPort(address)
	if err != nil || !endpoint.Addr().IsLoopback() {
		return errors.New("Node-context metrics require a literal loopback address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("cannot bind Node-context metrics listener")
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
		_, _ = io.WriteString(w, telemetry.Render())
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	failures := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- errors.New("Node-context metrics listener failed")
			cancel()
		}
	}()
	runErr := run(child)
	select {
	case err := <-failures:
		return err
	default:
		return runErr
	}
}
