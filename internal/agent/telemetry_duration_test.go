package agent

import (
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestTelemetryDurationMetricRetainsExactNanoseconds(t *testing.T) {
	for _, duration := range []time.Duration{
		0, time.Nanosecond, 999999999 * time.Nanosecond,
		1000522939 * time.Nanosecond, 2830801168 * time.Nanosecond,
		1800 * time.Second, -time.Nanosecond, -1000522939 * time.Nanosecond,
		time.Duration(math.MinInt64), time.Duration(math.MaxInt64),
	} {
		t.Run(duration.String(), func(t *testing.T) {
			telemetry := &Telemetry{}
			telemetry.RecordScan(time.Unix(1700000000, 0), duration, ScanResult{}, nil, 0)
			const prefix = "kubememlens_agent_last_scan_duration_seconds "
			var samples []string
			for line := range strings.SplitSeq(telemetry.Render(), "\n") {
				if strings.HasPrefix(line, prefix) {
					samples = append(samples, strings.TrimPrefix(line, prefix))
				}
			}
			if len(samples) != 1 {
				t.Fatal("one scan duration metric required")
			}
			value, ok := new(big.Rat).SetString(samples[0])
			if !ok {
				t.Fatal("scan duration is not a decimal number")
			}
			value.Mul(value, new(big.Rat).SetInt64(int64(time.Second)))
			if value.Cmp(new(big.Rat).SetInt64(int64(duration))) != 0 {
				t.Fatalf("metric changed scan duration: %s seconds for %d ns", samples[0], duration)
			}
		})
	}
}
