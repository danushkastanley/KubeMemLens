package main

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/delivery"
)

func loadExpectation(path string) (privateConfig, delivery.Expectation, error) {
	cfg, err := load(path)
	if err != nil || !sameBoot(cfg.WorkerBootID) {
		return cfg, delivery.Expectation{}, delivery.ErrObservation
	}
	spec, err := trace.NewSpecification(trace.Files, cfg.Target, trace.ConfirmedPaths,
		trace.Bounds{Duration: time.Duration(cfg.DurationSeconds) * time.Second, Events: cfg.MaxEvents,
			OutputBytes: cfg.MaxOutputBytes, MapBytes: cfg.MaxMapBytes, PathBytes: cfg.MaxPathBytes})
	if err != nil {
		return cfg, delivery.Expectation{}, delivery.ErrObservation
	}
	return cfg, delivery.Expectation{SessionID: cfg.SessionID, EngineDigest: cfg.EngineDigest,
		ProgrammeDigest: cfg.ProgrammeDigest, Specification: spec}, nil
}

func runObservation(ctx context.Context, mode, path string, output io.Writer) error {
	switch mode {
	case "normal":
		return run(ctx, path, output)
	case "ceiling":
		return runCeiling(ctx, path, output)
	case "paused-reader":
		return runPausedReader(ctx, path, output)
	default:
		return delivery.ErrObservation
	}
}

func runCeiling(ctx context.Context, path string, output io.Writer) error {
	cfg, expected, err := loadExpectation(path)
	if err != nil {
		return delivery.ErrObservation
	}
	result, observationErr := delivery.ConnectCeilingReady(ctx,
		delivery.Connection{Server: cfg.Server, Token: cfg.Token, CAPEM: cfg.CAPEM}, expected, func() error {
			return json.NewEncoder(output).Encode(struct {
				SchemaVersion int    `json:"schemaVersion"`
				Case          string `json:"case"`
				Ready         bool   `json:"ready"`
			}{1, "ceiling-ready", true})
		})
	clockMatched := sameBoot(cfg.WorkerBootID)
	document := struct {
		SchemaVersion   int                    `json:"schemaVersion"`
		Case            string                 `json:"case"`
		SameKernelClock bool                   `json:"sameKernelClock"`
		Observation     delivery.CeilingResult `json:"observation"`
	}{1, "ceiling-observation", clockMatched, result}
	if err := json.NewEncoder(output).Encode(document); err != nil {
		return delivery.ErrObservation
	}
	if observationErr != nil || !clockMatched || !result.TransportComplete || !result.CeilingReported {
		return delivery.ErrObservation
	}
	return nil
}
