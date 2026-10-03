package main

// Both independent bindings must be fully verified before any metrics read.
// Bound the concurrency to one peer goroutine and preserve agent-first errors.
func verifyBindings(agent, collector func() error) error {
	agentResult := make(chan error, 1)
	go func() { agentResult <- agent() }()
	collectorError := collector()
	agentError := <-agentResult
	if agentError != nil {
		return atStage("agent-binding", agentError)
	}
	if collectorError != nil {
		return atStage("collector-binding", collectorError)
	}
	return nil
}
