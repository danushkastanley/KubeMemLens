package resourcemetrics

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"

	"k8s.io/apimachinery/pkg/api/resource"
)

var exponentSuffix = regexp.MustCompile(`[eE]([+-]?[0-9]+)$`)

func wireUsageValue(raw json.RawMessage, decimalPlaces int) (uint64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 130 {
		return 0, false
	}
	text := string(raw)
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, false
		}
	}
	return usageValue(text, decimalPlaces)
}

// Metrics are bounded to int64 bytes or nanocores. Bound scientific exponents
// before Quantity parsing so a malicious provider cannot request huge rescaling.
func usageValue(text string, decimalPlaces int) (uint64, bool) {
	if text == "" || len(text) > 128 {
		return 0, false
	}
	if match := exponentSuffix.FindStringSubmatch(text); len(match) > 0 {
		exponent, err := strconv.Atoi(match[1])
		if err != nil || exponent < -18 || exponent > 18 {
			return 0, false
		}
	}
	quantity, err := resource.ParseQuantity(text)
	if err != nil || quantity.Sign() < 0 {
		return 0, false
	}
	scale := resource.Scale(-decimalPlaces)
	maximum := resource.NewScaledQuantity(math.MaxInt64, scale)
	if quantity.Cmp(*maximum) > 0 {
		return 0, false
	}
	value := quantity.ScaledValue(scale)
	if value < 0 {
		return 0, false
	}
	return uint64(value), true
}
