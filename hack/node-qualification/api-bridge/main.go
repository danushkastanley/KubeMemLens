// Command api-bridge supplies private, bounded reads to the qualification runner.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kube"
)

type options struct {
	kubeconfig, context, namespace, operation, node string
}

func main() {
	var opts options
	flag.StringVar(&opts.kubeconfig, "kubeconfig", "", "explicit private kubeconfig")
	flag.StringVar(&opts.context, "context", "", "explicit approved context")
	flag.StringVar(&opts.namespace, "namespace", "", "qualification namespace")
	flag.StringVar(&opts.operation, "operation", "", "status, containers, node or history")
	flag.StringVar(&opts.node, "node", "", "Node for node/history reads")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts, os.Stdout); err != nil {
		// The runner also bounds and keeps credential-plugin stderr private.
		fmt.Fprintln(os.Stderr, "qualification API read failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options, output io.Writer) error {
	if !filepath.IsAbs(opts.kubeconfig) || opts.context == "" || opts.namespace == "" {
		return errors.New("explicit target configuration is required")
	}
	if !validOperation(opts.operation) {
		return errors.New("unsupported qualification operation")
	}
	config, err := kube.BuildConfig(opts.kubeconfig, opts.context)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return read(ctx, config, opts, output)
}

func validOperation(operation string) bool {
	switch operation {
	case "status", "containers", "node", "history":
		return true
	default:
		return false
	}
}
