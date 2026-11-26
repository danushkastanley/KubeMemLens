package main

// A missing witness is unavailable evidence, not an empty attachment set.
type partialResult struct {
	BPFCalls      int        `json:"bpfCalls"`
	LinkEntries   int        `json:"linkEntries"`
	WorkersBefore []snapshot `json:"workersBefore"`
	Found         bool       `json:"found"`
	Before        *snapshot  `json:"before"`
	Action        *clockPair `json:"actionClock"`
	Signalled     int        `json:"signalled"`
	Observations  int        `json:"observations"`
}

func closeProcesses(workers []*process) error {
	var result error
	for _, worker := range workers {
		if worker.close() != nil {
			result = errOwnership
		}
	}
	return result
}

func sameChildren(parent *process, workers []*process, targets map[uint64]bool) error {
	ids, err := children(parent)
	if err != nil || len(ids) != len(workers) {
		return errOwnership
	}
	for _, worker := range workers {
		found := false
		for _, id := range ids {
			if id == worker.pid {
				found = true
			}
		}
		if !found || worker.check() != nil || !selectedTarget(worker.pid, targets) {
			return errOwnership
		}
	}
	return nil
}
