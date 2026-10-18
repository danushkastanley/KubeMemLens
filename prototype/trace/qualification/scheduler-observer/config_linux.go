package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/scheduler"
)

var errInput = errors.New("scheduler input or source binding invalid")
var names = []string{"sched_wakeup", "sched_wakeup_new", "sched_switch", "sched_process_exit"}
var bootPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type configuration struct {
	Anchor           anchorConfiguration `json:"anchor"`
	Seconds          int                 `json:"seconds"`
	BootID           string              `json:"bootID"`
	OnlineCPUs       string              `json:"onlineCPUs"`
	TracepointSHA256 map[string]string   `json:"tracepointSHA256"`
}

func boundedRead(path string, maximum int, private bool) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errInput
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (private && info.Mode().Perm()&0077 != 0) {
		return nil, errInput
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(maximum+1)))
	if err != nil || len(data) > maximum {
		return nil, errInput
	}
	return data, nil
}

func unique(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	var object func(string) bool
	object = func(kind string) bool {
		required := names
		switch kind {
		case "root":
			required = []string{"seconds", "bootID", "onlineCPUs", "tracepointSHA256", "anchor"}
		case "anchor":
			required = []string{"pid", "start"}
		}
		token, err := d.Token()
		if err != nil || token != json.Delim('{') {
			return false
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] || len(seen) >= len(required) {
				return false
			}
			valid := false
			for _, field := range required {
				if name == field {
					valid = true
					break
				}
			}
			if !valid {
				return false
			}
			seen[name] = true
			if kind == "root" && (name == "tracepointSHA256" || name == "anchor") {
				if !object(name) {
					return false
				}
				continue
			}
			value, err := d.Token()
			if err != nil {
				return false
			}
			if _, ok := value.(json.Delim); ok {
				return false
			}
		}
		token, err = d.Token()
		return err == nil && token == json.Delim('}') && len(seen) == len(required)
	}
	if !object("root") {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

func cpuList(text string) ([]uint32, error) {
	if len(text) == 0 || len(text) > 512 {
		return nil, errInput
	}
	cpus := []uint32{}
	for _, part := range strings.Split(text, ",") {
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, errInput
		}
		first, err := strconv.ParseUint(bounds[0], 10, 12)
		if err != nil {
			return nil, errInput
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.ParseUint(bounds[1], 10, 12)
			if err != nil || last <= first {
				return nil, errInput
			}
		}
		if last-first+1 > 64-uint64(len(cpus)) {
			return nil, errInput
		}
		for n := first; n <= last; n++ {
			if len(cpus) > 0 && n <= uint64(cpus[len(cpus)-1]) {
				return nil, errInput
			}
			cpus = append(cpus, uint32(n))
		}
	}
	return cpus, nil
}

func load(path string) (configuration, error) {
	var cfg configuration
	raw, err := boundedRead(path, 16384, true)
	if err != nil || !unique(raw) {
		return cfg, errInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || cfg.Seconds < 1 || cfg.Seconds > 1800 || !bootPattern.MatchString(cfg.BootID) || cfg.Anchor.PID <= 1 || cfg.Anchor.PID > 0x7fffffff || cfg.Anchor.Start == 0 {
		return cfg, errInput
	}
	if _, err := cpuList(cfg.OnlineCPUs); err != nil {
		return cfg, err
	}
	for _, name := range names {
		if !hashPattern.MatchString(cfg.TracepointSHA256[name]) {
			return cfg, errInput
		}
	}
	return cfg, nil
}

func (cfg configuration) identities() error {
	for path, expected := range map[string]string{"/proc/sys/kernel/random/boot_id": cfg.BootID, "/sys/devices/system/cpu/online": cfg.OnlineCPUs} {
		data, err := boundedRead(path, 512, false)
		if err != nil || strings.TrimSpace(string(data)) != expected {
			return errInput
		}
	}
	return nil
}

func (cfg configuration) formats() ([]scheduler.Format, error) {
	var formats []scheduler.Format
	for _, name := range names {
		raw, err := boundedRead("/sys/kernel/tracing/events/sched/"+name+"/format", 16384, false)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != cfg.TracepointSHA256[name] {
			return nil, errInput
		}
		format, err := scheduler.ParseFormat(string(raw))
		if err != nil {
			return nil, err
		}
		formats = append(formats, format)
	}
	return formats, nil
}
