// Administrative qualification only; raw metrics never enter output records.
package main

import (
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var errObservation = errors.New("standard metrics observation unavailable")
var metricNumber = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,2})?$`)

var agentFields = map[string]string{
	`scans_total{result="success"}`: "scanSuccess", `scans_total{result="failure"}`: "scanFailure",
	`snapshot_posts_total{result="success"}`: "postSuccess", `snapshot_posts_total{result="failure"}`: "postFailure",
	"last_scan_timestamp_seconds": "scanCompletedUnixSeconds", "last_scan_duration_seconds": "scanDurationNanos",
	`last_scan_containers{kind="found"}`: "found", `last_scan_containers{kind="mapped"}`: "mapped",
	`last_scan_containers{kind="unmapped"}`: "unmapped", `last_scan_containers{kind="infrastructure"}`: "infrastructure",
	"metadata_cache_pods": "metadataCachePods",
}
var resultNames = []string{"accepted", "duplicate", "identity_rejected", "method_not_allowed", "content_encoding_rejected", "unsupported_media_type", "payload_too_large", "rate_limited", "concurrency_limited", "body_error", "invalid_json", "invalid_snapshot", "store_error", "out_of_order", "store_capacity"}

func metricValue(raw string, duration bool) (uint64, error) {
	if len(raw) > 64 || !metricNumber.MatchString(raw) {
		return 0, errObservation
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return 0, errObservation
	}
	ceiling := new(big.Rat).SetInt64(1<<53 - 1)
	if duration {
		ceiling.SetInt64(1800)
	}
	if value.Sign() < 0 || value.Cmp(ceiling) > 0 {
		return 0, errObservation
	}
	if duration {
		value.Mul(value, new(big.Rat).SetInt64(1000000000))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(value.Num(), value.Denom(), remainder)
		if remainder.Sign() > 0 {
			quotient.Add(quotient, big.NewInt(1))
		}
		return quotient.Uint64(), nil
	}
	if !value.IsInt() {
		return 0, errObservation
	}
	return value.Num().Uint64(), nil
}

func projectMetrics(data []byte, prefix string, fields map[string]string, durationField string) (map[string]uint64, error) {
	if len(data) > 1<<20 || !utf8.Valid(data) {
		return nil, errObservation
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) > 8192 || len(lines) == 0 || lines[len(lines)-1] != "# EOF" {
		return nil, errObservation
	}
	families := map[string]bool{}
	for name := range fields {
		families[strings.SplitN(name, "{", 2)[0]] = true
	}
	result := map[string]uint64{}
	eof := 0
	for _, line := range lines {
		if line == "# EOF" {
			eof++
			continue
		}
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		parts := strings.Fields(line)
		name := strings.TrimPrefix(parts[0], prefix)
		if !families[strings.SplitN(name, "{", 2)[0]] {
			continue
		}
		out, ok := fields[name]
		if !ok || len(parts) != 2 {
			return nil, errObservation
		}
		if _, exists := result[out]; exists {
			return nil, errObservation
		}
		value, err := metricValue(parts[1], out == durationField)
		if err != nil {
			return nil, err
		}
		result[out] = value
	}
	if eof != 1 {
		return nil, errObservation
	}
	return result, nil
}

func parseAgent(data []byte) (map[string]uint64, error) {
	result, err := projectMetrics(data, "kubememlens_agent_", agentFields, "scanDurationNanos")
	if err != nil || len(result) != len(agentFields) || result["scanSuccess"]+result["scanFailure"] == 0 || result["scanCompletedUnixSeconds"] == 0 || result["scanDurationNanos"] == 0 {
		return nil, errObservation
	}
	return result, nil
}

type collectorObservation struct {
	DurationNanos uint64            `json:"durationNanos"`
	Results       map[string]uint64 `json:"results"`
}

func parseCollector(data []byte) (collectorObservation, error) {
	fields := map[string]string{"last_duration_seconds": "durationNanos"}
	for _, name := range resultNames {
		fields[`requests_total{result=`+strconv.Quote(name)+`}`] = name
	}
	result, err := projectMetrics(data, "kubememlens_collector_ingestion_", fields, "durationNanos")
	if err != nil || len(result) < 2 || result["durationNanos"] == 0 {
		return collectorObservation{}, errObservation
	}
	value := collectorObservation{DurationNanos: result["durationNanos"], Results: map[string]uint64{}}
	var total uint64
	for _, name := range resultNames {
		value.Results[name] = result[name]
		total += result[name]
	}
	if total == 0 || total > 1<<53-1 {
		return collectorObservation{}, errObservation
	}
	return value, nil
}
