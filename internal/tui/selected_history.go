package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type selectedHistory struct {
	namespace  string
	podName    string
	generation uint64
	inFlight   bool
	loading    bool
	series     []api.PodHistory
	err        error
	updatedAt  time.Time
}

func (state *selectedHistory) selectPod(namespace, podName string) {
	if state.namespace == namespace && state.podName == podName {
		return
	}
	state.namespace = namespace
	state.podName = podName
	state.generation++
	state.inFlight = false
	state.loading = namespace != "" && podName != ""
	state.series = nil
	state.err = nil
	state.updatedAt = time.Time{}
}

func (state *selectedHistory) clearSelection() {
	state.selectPod("", "")
	state.loading = false
}

func (state *selectedHistory) start() (historyRequest, bool) {
	if state.namespace == "" || state.podName == "" || state.inFlight {
		return historyRequest{}, false
	}
	state.inFlight = true
	state.loading = len(state.series) == 0
	return historyRequest{namespace: state.namespace, podName: state.podName, generation: state.generation}, true
}

func (state *selectedHistory) complete(message historyMsg, now time.Time) bool {
	if message.namespace != state.namespace || message.podName != state.podName || message.generation != state.generation {
		return false
	}
	state.inFlight = false
	state.loading = false
	state.err = message.err
	if message.err == nil {
		state.series = message.series
		state.updatedAt = now
	}
	return true
}

type historyRequest struct {
	namespace  string
	podName    string
	generation uint64
}
