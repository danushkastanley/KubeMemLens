package main

import (
	"bytes"
	"encoding/json"
	"io"
)

type scanTiming struct {
	Sequence           uint64 `json:"sequence"`
	CompletedUnixNanos int64  `json:"completedUnixNanos"`
	DurationNanos      int64  `json:"durationNanos"`
	Result             string `json:"result"`
}

type agentObservation struct {
	Metrics map[string]uint64 `json:"metrics"`
	Scans   []scanTiming      `json:"recentScans"`
}

func decodeObservation(data []byte, value any) error {
	if len(data) > 16384 || boundedJSON(data, 32) != nil {
		return errObservation
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return errObservation
	}
	return nil
}

func parseScanObservations(data []byte) (agentObservation, error) {
	var envelope struct {
		SchemaVersion int          `json:"schemaVersion"`
		Metrics       string       `json:"metrics"`
		RecentScans   []scanTiming `json:"recentScans"`
	}
	if decodeObservation(data, &envelope) != nil || envelope.SchemaVersion != 1 {
		return agentObservation{}, errObservation
	}
	metrics, err := parseAgent([]byte(envelope.Metrics))
	if err != nil {
		return agentObservation{}, err
	}
	value := agentObservation{metrics, envelope.RecentScans}
	return value, validateScanObservations(value)
}

func validateScanObservations(value agentObservation) error {
	if len(value.Metrics) != len(agentFields) {
		return errObservation
	}
	for _, name := range agentFields {
		if metric, ok := value.Metrics[name]; !ok || metric > 1<<53-1 {
			return errObservation
		}
	}
	total := value.Metrics["scanSuccess"] + value.Metrics["scanFailure"]
	if total == 0 || total > 1<<53-1 || uint64(len(value.Scans)) != min(total, 32) {
		return errObservation
	}
	var completed int64
	var successes, failures uint64
	for index, scan := range value.Scans {
		if scan.Sequence != total-uint64(len(value.Scans))+uint64(index)+1 || scan.DurationNanos <= 0 ||
			scan.DurationNanos > 1800*1000000000 || scan.CompletedUnixNanos < scan.DurationNanos ||
			scan.CompletedUnixNanos < completed {
			return errObservation
		}
		switch scan.Result {
		case "success":
			successes++
		case "failure":
			failures++
		default:
			return errObservation
		}
		completed = scan.CompletedUnixNanos
	}
	last := value.Scans[len(value.Scans)-1]
	if uint64(last.DurationNanos) != value.Metrics["scanDurationNanos"] {
		return atStage("agent-scan-duration", errObservation)
	}
	if successes > value.Metrics["scanSuccess"] || failures > value.Metrics["scanFailure"] ||
		uint64(last.CompletedUnixNanos/1000000000) != value.Metrics["scanCompletedUnixSeconds"] {
		return errObservation
	}
	return nil
}
