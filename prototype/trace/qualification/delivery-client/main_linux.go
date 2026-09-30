// Local qualification only. Attaching to an approved admission starts its worker.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/delivery"
)

type privateConfig struct {
	Server          string               `json:"server"`
	Token           string               `json:"token"`
	CAPEM           string               `json:"caPEM"`
	WorkerBootID    string               `json:"workerBootID"`
	SessionID       string               `json:"sessionID"`
	EngineDigest    string               `json:"engineDigest"`
	ProgrammeDigest string               `json:"programmeDigest"`
	Target          trace.TargetIdentity `json:"target"`
	DurationSeconds int                  `json:"durationSeconds"`
	MaxEvents       uint64               `json:"maxEvents"`
	MaxOutputBytes  uint64               `json:"maxOutputBytes"`
	MaxMapBytes     uint64               `json:"maxMapBytes"`
	MaxPathBytes    uint64               `json:"maxPathBytes"`
}

func (privateConfig) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private delivery configuration]")
}
func (privateConfig) MarshalJSON() ([]byte, error) { return nil, delivery.ErrObservation }

var bootID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func load(path string) (privateConfig, error) {
	var cfg privateConfig
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return cfg, delivery.ErrObservation
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return cfg, delivery.ErrObservation
	}
	raw, err := io.ReadAll(io.LimitReader(file, 32769))
	if err != nil || len(raw) > 32768 {
		return cfg, delivery.ErrObservation
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF || !bootID.MatchString(cfg.WorkerBootID) || cfg.DurationSeconds < 1 || cfg.DurationSeconds > 30 {
		return cfg, delivery.ErrObservation
	}
	return cfg, nil
}
func sameBoot(expected string) bool {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return err == nil && len(data) <= 64 && strings.TrimSpace(string(data)) == expected
}
func run(ctx context.Context, path string, output io.Writer) error {
	cfg, err := load(path)
	if err != nil || !sameBoot(cfg.WorkerBootID) {
		return delivery.ErrObservation
	}
	spec, err := trace.NewSpecification(trace.Files, cfg.Target, trace.ConfirmedPaths, trace.Bounds{Duration: time.Duration(cfg.DurationSeconds) * time.Second, Events: cfg.MaxEvents, OutputBytes: cfg.MaxOutputBytes, MapBytes: cfg.MaxMapBytes, PathBytes: cfg.MaxPathBytes})
	if err != nil {
		return delivery.ErrObservation
	}
	result, observationErr := delivery.Connect(ctx, delivery.Connection{Server: cfg.Server, Token: cfg.Token, CAPEM: cfg.CAPEM}, delivery.Expectation{SessionID: cfg.SessionID, EngineDigest: cfg.EngineDigest, ProgrammeDigest: cfg.ProgrammeDigest, Specification: spec})
	clockMatched := sameBoot(cfg.WorkerBootID)
	var measured *delivery.Latency
	if observationErr == nil && clockMatched {
		value, err := delivery.Latencies(result)
		if err != nil {
			observationErr = err
		} else {
			measured = &value
		}
	}
	document := struct {
		SchemaVersion   int               `json:"schemaVersion"`
		SameKernelClock bool              `json:"sameKernelClock"`
		Observation     delivery.Result   `json:"observation"`
		Latency         *delivery.Latency `json:"latency,omitempty"`
	}{1, clockMatched, result, measured}
	if err := json.NewEncoder(output).Encode(document); err != nil {
		return delivery.ErrObservation
	}
	if observationErr != nil || !clockMatched || measured == nil || !measured.Passed {
		return delivery.ErrObservation
	}
	return nil
}
func main() {
	deadline := time.AfterFunc(45*time.Second, func() { os.Exit(2) })
	defer deadline.Stop()
	flags := flag.NewFlagSet("delivery-client", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "private approved-admission configuration")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "invalid delivery-client arguments")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if run(ctx, *path, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "delivery qualification incomplete or failed; retain the numeric result")
		os.Exit(1)
	}
}
