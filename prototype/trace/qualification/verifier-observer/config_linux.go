package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier"
)

var errInput = errors.New("verifier input or source binding invalid")
var bootPattern = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type sourceMode uint8

const (
	privateConfig sourceMode = iota
	kernelData
)

type anchorConfiguration struct {
	PID    int    `json:"pid"`
	Start  uint64 `json:"start"`
	SHA256 string `json:"sha256"`
}
type groupConfiguration struct {
	Path  string `json:"path"`
	Inode uint64 `json:"inode"`
}
type configuration struct {
	Owner      string              `json:"owner"`
	Seconds    int                 `json:"seconds"`
	BootID     string              `json:"bootID"`
	OnlineCPUs string              `json:"onlineCPUs"`
	BTFSHA256  string              `json:"btfSHA256"`
	Anchor     anchorConfiguration `json:"anchor"`
	Group      groupConfiguration  `json:"group"`
}

func boundedRead(path string, maximum int, mode sourceMode) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errInput
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (mode == privateConfig && info.Mode().Perm()&0077 != 0) {
		_ = f.Close()
		return nil, errInput
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, int64(maximum+1)))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(raw) > maximum {
		return nil, errInput
	}
	return raw, nil
}

func unique(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	schemas := map[string][]string{"root": {"owner", "seconds", "bootID", "onlineCPUs", "btfSHA256", "anchor", "group"},
		"anchor": {"pid", "start", "sha256"}, "group": {"path", "inode"}}
	var object func(string) bool
	object = func(kind string) bool {
		token, err := decoder.Token()
		if err != nil || token != json.Delim('{') {
			return false
		}
		keys := map[string]bool{}
		for _, name := range schemas[kind] {
			keys[name] = false
		}
		for decoder.More() {
			token, err := decoder.Token()
			name, ok := token.(string)
			seen, known := keys[name]
			if err != nil || !ok || !known || seen {
				return false
			}
			keys[name] = true
			if kind == "root" && (name == "anchor" || name == "group") {
				if !object(name) {
					return false
				}
				continue
			}
			token, err = decoder.Token()
			if err != nil || token == nil {
				return false
			}
			if _, nested := token.(json.Delim); nested {
				return false
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return false
		}
		for _, seen := range keys {
			if !seen {
				return false
			}
		}
		return true
	}
	if !object("root") {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func load(path string) (configuration, error) {
	var cfg configuration
	raw, err := boundedRead(path, 16384, privateConfig)
	if err != nil || !unique(raw) {
		return cfg, errInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || cfg.Seconds < 1 || cfg.Seconds > 1800 || !bootPattern.MatchString(cfg.BootID) ||
		!hashPattern.MatchString(cfg.BTFSHA256) || cfg.Anchor.PID <= 1 || cfg.Anchor.PID > 0x7fffffff || cfg.Anchor.Start == 0 ||
		!hashPattern.MatchString(cfg.Anchor.SHA256) || cfg.Group.Inode == 0 {
		return cfg, errInput
	}
	if _, err := verifier.NewProbePlan(cfg.Owner); err != nil {
		return cfg, errInput
	}
	if _, err := cpuList(cfg.OnlineCPUs); err != nil {
		return cfg, err
	}
	if len(cfg.Group.Path) > 4096 || !strings.HasPrefix(cfg.Group.Path, "/sys/fs/cgroup/") ||
		filepath.Clean(cfg.Group.Path) != cfg.Group.Path {
		return cfg, errInput
	}
	return cfg, nil
}

func (cfg configuration) identities() error {
	for path, expected := range map[string]string{"/proc/sys/kernel/random/boot_id": cfg.BootID, "/sys/devices/system/cpu/online": cfg.OnlineCPUs} {
		raw, err := boundedRead(path, 512, kernelData)
		if err != nil || strings.TrimSpace(string(raw)) != expected {
			return errInput
		}
	}
	return nil
}
