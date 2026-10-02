package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

type limitedBuffer struct {
	bytes.Buffer
	maximum int
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.maximum-b.Len() {
		return 0, errObservation
	}
	return b.Buffer.Write(data)
}

func readAgent(ctx context.Context, p *boundProcess) (agentObservation, error) {
	// Pin the namespace and executable descriptors before forking. nsenter and
	// the child receive no token/config, and can only fetch this fixed loopback URL.
	ns, err := os.Open(procPath(p.pid, "ns/net"))
	if err != nil {
		return agentObservation{}, atStage("agent-binding", err)
	}
	defer ns.Close()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return agentObservation{}, atStage("agent-child", err)
	}
	defer self.Close()
	if p.alive() != nil {
		return agentObservation{}, atStage("agent-binding", errObservation)
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(deadline, "/usr/bin/nsenter", "--net=/proc/self/fd/3", "--", "/proc/self/fd/4", "--agent-scrape")
	cmd.ExtraFiles = []*os.File{ns, self}
	cmd.Env = []string{"LANG=C"}
	diagnostic := &limitedBuffer{maximum: 512}
	cmd.Stderr = diagnostic
	out := &limitedBuffer{maximum: 8192}
	cmd.Stdout = out
	if cmd.Run() != nil {
		stage := safeStage(strings.TrimSpace(diagnostic.String()))
		if strings.HasPrefix(stage, "agent-") {
			return agentObservation{}, atStage(stage, errObservation)
		}
		return agentObservation{}, atStage("agent-child", errObservation)
	}
	if p.alive() != nil {
		return agentObservation{}, atStage("agent-binding", errObservation)
	}
	var value agentObservation
	if decodeObservation(out.Bytes(), &value) != nil || validateScanObservations(value) != nil {
		return agentObservation{}, atStage("agent-projection", errObservation)
	}
	return value, nil
}
