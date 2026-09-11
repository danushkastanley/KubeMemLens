package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/nodestats"
	"k8s.io/client-go/rest"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("memlens-node-context", flag.ContinueOnError)
	flags.SetOutput(stderr)
	node := flags.String("node-name", os.Getenv("NODE_NAME"), "scheduled Kubernetes Node name")
	ca := flags.String("kubelet-ca", "", "explicit kubelet serving CA bundle file")
	token := flags.String("kubelet-token-file", "", "rotating Pod-bound token with a qualified kubelet audience")
	timeout := flags.Duration("timeout", nodecontext.RequestTimeout, "whole-read timeout, at most five seconds")
	once := flags.Bool("once", false, "collect one normalised observation without publishing")
	output := flags.String("output", "", "new private observation file; empty writes JSON to stdout")
	metrics := flags.String("metrics-output", "", "optional new private file for operational metrics")
	version := flags.Bool("version", false, "print build information and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *version {
		_, err := fmt.Fprintln(stdout, buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String())
		return err
	}
	if !*once {
		return errors.New("publishing is not available yet; use --once for explicit collection")
	}
	if *node == "" || *ca == "" || *token == "" {
		return errors.New("node name, kubelet CA and kubelet token file are required")
	}
	if err := checkLocalPlatform(); err != nil {
		return err
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return &nodestats.Error{Reason: nodecontext.Authentication}
	}
	config.UserAgent = "kube-memlens-node-context/" + buildinfo.Version
	telemetry := &nodestats.Telemetry{}
	source, err := nodestats.New(config, nodestats.Options{NodeName: *node, CAFile: *ca, TokenFile: *token, Timeout: *timeout, Telemetry: telemetry})
	if err != nil {
		return err
	}
	defer source.Close()
	report, readErr := source.Read(ctx)
	if *metrics != "" {
		if err := writePrivate(*metrics, []byte(telemetry.Render())); err != nil {
			return err
		}
	}
	if readErr != nil {
		return readErr
	}
	encoded, err := diagnosticBytes(report)
	if err != nil || len(encoded) > nodecontext.MaxObservationBytes {
		return &nodestats.Error{Reason: nodecontext.ResponseTooLarge}
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		return writePrivate(*output, encoded)
	}
	_, err = stdout.Write(encoded)
	return err
}

func diagnosticBytes(report nodecontext.Observation) ([]byte, error) {
	// Internal identity remains available to the authenticated publisher, but
	// a diagnostic export follows the default capture redaction policy.
	report.NodeUID = ""
	return json.Marshal(struct {
		Kind          string                  `json:"kind"`
		SchemaVersion int                     `json:"schemaVersion"`
		Redacted      bool                    `json:"redacted"`
		Observation   nodecontext.Observation `json:"observation"`
	}{Kind: "NodeContextDiagnostic", SchemaVersion: 1, Redacted: true, Observation: report})
}

func writePrivate(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create output file; choose a new writable path")
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return errors.New("cannot finish output file")
	}
	return nil
}
