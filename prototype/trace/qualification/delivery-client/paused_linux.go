package main

import (
	"context"
	"encoding/json"
	"io"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/delivery"
)

func runPausedReader(ctx context.Context, path string, output io.Writer) error {
	cfg, expected, err := loadExpectation(path)
	if err != nil {
		return delivery.ErrObservation
	}
	result, observationErr := delivery.ConnectPausedReaderReady(ctx,
		delivery.Connection{Server: cfg.Server, Token: cfg.Token, CAPEM: cfg.CAPEM}, expected, func() error {
			return json.NewEncoder(output).Encode(struct {
				SchemaVersion int    `json:"schemaVersion"`
				Case          string `json:"case"`
				Ready         bool   `json:"ready"`
			}{1, "paused-reader-ready", true})
		})
	clockMatched := sameBoot(cfg.WorkerBootID)
	document := struct {
		SchemaVersion   int                    `json:"schemaVersion"`
		Case            string                 `json:"case"`
		SameKernelClock bool                   `json:"sameKernelClock"`
		Pause           delivery.ReaderPause   `json:"pause"`
		Observation     delivery.CeilingResult `json:"observation"`
	}{1, "paused-reader-observation", clockMatched, result.Pause, result.Observation}
	if err := json.NewEncoder(output).Encode(document); err != nil {
		return delivery.ErrObservation
	}
	if observationErr != nil || !clockMatched || !result.Pause.Completed || !result.Observation.TransportComplete || !result.Observation.CeilingReported {
		return delivery.ErrObservation
	}
	return nil
}
