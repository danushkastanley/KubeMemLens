package agent

import (
	"fmt"
	"strings"
	"time"
)

// Keep the decimal metric equal to the nanosecond observation. Converting via
// Duration.Seconds can add a floating-point remainder to otherwise exact data.
func scanDurationSeconds(duration time.Duration) string {
	seconds, nanos := duration/time.Second, duration%time.Second
	sign := ""
	if duration < 0 {
		sign, seconds, nanos = "-", -seconds, -nanos
	}
	value := fmt.Sprintf("%s%d.%09d", sign, seconds, nanos)
	return strings.TrimSuffix(strings.TrimRight(value, "0"), ".")
}
