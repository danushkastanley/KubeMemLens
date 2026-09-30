package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
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

func readAgent(ctx context.Context, p *boundProcess) (map[string]uint64, error) {
	// Pin the namespace and executable descriptors before forking. nsenter and
	// the child receive no token/config, and can only fetch this fixed loopback URL.
	ns, err := os.Open(procPath(p.pid, "ns/net"))
	if err != nil {
		return nil, errObservation
	}
	defer ns.Close()
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		return nil, errObservation
	}
	defer self.Close()
	if p.alive() != nil {
		return nil, errObservation
	}
	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(deadline, "/usr/bin/nsenter", "--net=/proc/self/fd/3", "--", "/proc/self/fd/4", "--agent-scrape")
	cmd.ExtraFiles = []*os.File{ns, self}
	cmd.Env = []string{"LANG=C"}
	cmd.Stderr = io.Discard
	out := &limitedBuffer{maximum: 4096}
	cmd.Stdout = out
	if cmd.Run() != nil || p.alive() != nil {
		return nil, errObservation
	}
	var value map[string]uint64
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	if dec.Decode(&value) != nil || dec.Decode(new(any)) != io.EOF || len(value) != len(agentFields) {
		return nil, errObservation
	}
	for _, field := range agentFields {
		if _, ok := value[field]; !ok {
			return nil, errObservation
		}
	}
	return value, nil
}
