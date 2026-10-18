package main

import (
	"errors"
	"strconv"
	"strings"
)

// A missing controller file is distinct from an explicitly unlimited setting.
type observedLimit struct {
	State string  `json:"state"`
	Value *uint64 `json:"value,omitempty"`
}

type cpuLimit struct {
	Maximum observedLimit `json:"maximum"`
	Period  uint64        `json:"periodUsec"`
}

type containmentSample struct {
	CPU        cpuLimit      `json:"cpu"`
	CPUBurst   observedLimit `json:"cpuBurstUsec"`
	MemoryMax  observedLimit `json:"memoryMaxBytes"`
	MemoryPeak observedLimit `json:"memoryPeakBytes"`
	PIDsMax    observedLimit `json:"pidsMax"`
}

func numericObservation(text string) (observedLimit, error) {
	if text == "" || strings.Trim(text, "0123456789") != "" {
		return observedLimit{}, errors.New("invalid numeric controller observation")
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return observedLimit{}, errors.New("controller observation overflows")
	}
	return observedLimit{State: "observed", Value: &value}, nil
}

func maximumObservation(text string) (observedLimit, error) {
	if text == "max" {
		return observedLimit{State: "unlimited"}, nil
	}
	return numericObservation(text)
}

func parseCPULimit(raw []byte) (cpuLimit, error) {
	fields := strings.Fields(string(raw))
	if len(fields) != 2 {
		return cpuLimit{}, errors.New("invalid CPU bandwidth observation")
	}
	maximum, err := maximumObservation(fields[0])
	if err != nil || maximum.Value != nil && *maximum.Value == 0 {
		return cpuLimit{}, errors.New("invalid CPU maximum")
	}
	period, err := numericObservation(fields[1])
	if err != nil || *period.Value == 0 {
		return cpuLimit{}, errors.New("invalid CPU period")
	}
	return cpuLimit{Maximum: maximum, Period: *period.Value}, nil
}
