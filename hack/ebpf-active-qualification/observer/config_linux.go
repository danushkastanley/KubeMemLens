package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
)

type config struct {
	Seconds            int    `json:"seconds"`
	BootID             string `json:"bootID"`
	Server             string `json:"server"`
	Token              string `json:"token"`
	CAPEM              string `json:"caPEM"`
	AgentPID           int    `json:"agentPID"`
	AgentStart         uint64 `json:"agentStart"`
	AgentContainer     string `json:"agentContainer"`
	AgentSHA256        string `json:"agentSHA256"`
	CollectorPID       int    `json:"collectorPID"`
	CollectorStart     uint64 `json:"collectorStart"`
	CollectorContainer string `json:"collectorContainer"`
	CollectorSHA256    string `json:"collectorSHA256"`
}

func (config) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private metrics configuration]")
}
func (config) MarshalJSON() ([]byte, error) { return nil, errObservation }

var bootPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func loadConfig(path string) (config, error) {
	var cfg config
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return cfg, errObservation
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return cfg, errObservation
	}
	data, err := io.ReadAll(io.LimitReader(f, 32769))
	if err != nil || len(data) > 32768 {
		return cfg, errObservation
	}
	if uniqueConfig(data) != nil {
		return cfg, errObservation
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&cfg) != nil || dec.Decode(new(any)) != io.EOF || cfg.Seconds < 1 || cfg.Seconds > 1800 || !bootPattern.MatchString(cfg.BootID) || cfg.AgentPID == cfg.CollectorPID {
		return cfg, errObservation
	}
	return cfg, nil
}
func uniqueConfig(data []byte) error {
	allowed := strings.Fields("seconds bootID server token caPEM agentPID agentStart agentContainer agentSHA256 collectorPID collectorStart collectorContainer collectorSHA256")
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return errObservation
	}
	seen := map[string]bool{}
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return errObservation
		}
		valid := false
		for _, name := range allowed {
			if key == name {
				valid = true
				break
			}
		}
		if !valid {
			return errObservation
		}
		seen[key] = true
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return errObservation
		}
	}
	token, err = dec.Token()
	if err != nil || token != json.Delim('}') || len(seen) != len(allowed) {
		return errObservation
	}
	return nil
}
func sameBoot(expected string) bool {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return err == nil && len(data) <= 64 && strings.TrimSpace(string(data)) == expected
}
