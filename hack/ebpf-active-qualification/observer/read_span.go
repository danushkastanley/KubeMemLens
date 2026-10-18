package main

import "fmt"

const maximumReadNanos int64 = 100000000

type readStages struct {
	bindings, agent, collector, final int64
}

type sampledMetrics struct {
	agent     agentObservation
	collector collectorObservation
	stages    readStages
}

// Only numeric elapsed times are retained after the unchanged read bound fails.
// These are diagnostic stage costs, not replacement observations or a retry.
type readSpanFailure struct {
	index  int
	nanos  int64
	stages readStages
}

func (readSpanFailure) Error() string { return "read-span" }

func (f readSpanFailure) detail() string {
	return fmt.Sprintf("read-span index=%d nanos=%d bindings_nanos=%d agent_nanos=%d collector_nanos=%d final_binding_nanos=%d",
		f.index, f.nanos, f.stages.bindings, f.stages.agent, f.stages.collector, f.stages.final)
}

func checkReadSpan(index int, nanos int64, stages readStages) error {
	if nanos >= 1 && nanos <= maximumReadNanos {
		return nil
	}
	return readSpanFailure{index, nanos, stages}
}
