package verifier

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"

	"github.com/cilium/ebpf/btf"
)

type ProbeSet struct {
	lease     *probeLease
	fs        *traceFS
	btfSHA256 string
	closed    bool
	closeErr  error
}

// RegisterProbeSet creates only the fixed uniquely named definitions, initially
// disabled. It never writes a global enable flag or attaches a BPF programme.
// The caller must first journal the owner token and enforce an owned-node scope,
// boot/process binding, finite deadline and external cleanup after forced exit.
func RegisterProbeSet(plan ProbePlan, expectedBTF string) (*ProbeSet, error) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return nil, ErrObservation
	}
	hash, err := kernelABI(expectedBTF)
	if err != nil {
		return nil, err
	}
	fs, err := openTraceFS(plan)
	if err != nil {
		return nil, err
	}
	lease, err := installProbes(plan, fs)
	p := &ProbeSet{lease: lease, fs: fs, btfSHA256: hash}
	if err != nil {
		return nil, errors.Join(err, p.Close())
	}
	definitions, _ := plan.Definitions()
	for _, d := range definitions {
		enabled, err := fs.read("events/" + d.Name + "/enable")
		if err != nil || string(enabled) != "0\n" {
			return nil, errors.Join(ErrObservation, p.Close())
		}
	}
	return p, nil
}

func (p *ProbeSet) BTFSHA256() string { return p.btfSHA256 }

func kernelABI(expected string) (string, error) {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != expected {
		return "", ErrObservation
	}
	file, err := os.Open("/sys/kernel/btf/vmlinux")
	if err != nil {
		return "", ErrObservation
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, 64<<20+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(raw) == 0 || len(raw) > 64<<20 {
		return "", ErrObservation
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expected {
		return "", ErrObservation
	}
	spec, err := btf.LoadSpecFromReader(bytes.NewReader(raw))
	if err != nil || ValidateABI(spec) != nil {
		return "", ErrObservation
	}
	return expected, nil
}

// Formats projects only validated layouts. Hashes let the caller preserve the
// exact kernel schemas without dumping raw event payloads or addresses.
func (p *ProbeSet) Formats() ([]Format, map[string]string, error) {
	if p.closed || p.lease.verify() != nil {
		return nil, nil, ErrObservation
	}
	definitions, _ := p.lease.plan.Definitions()
	kinds := []Kind{CheckEnter, CheckReturn, LogFinalized}
	formats, hashes := make([]Format, 0, 3), make(map[string]string, 3)
	ids := make(map[uint16]bool)
	for index, definition := range definitions {
		raw, err := p.fs.read("events/" + definition.Name + "/format")
		if err != nil {
			return nil, nil, err
		}
		format, err := p.lease.plan.ParseFormat(string(raw), kinds[index])
		if err != nil || ids[format.id] {
			return nil, nil, ErrObservation
		}
		ids[format.id] = true
		formats = append(formats, format)
		hash := sha256.Sum256(raw)
		hashes[definition.Name] = hex.EncodeToString(hash[:])
	}
	return formats, hashes, nil
}

func (p *ProbeSet) Counts() (map[string]ProbeCount, error) {
	if p.closed || p.lease.verify() != nil {
		return nil, ErrObservation
	}
	raw, err := p.fs.read("kprobe_profile")
	if err != nil {
		return nil, err
	}
	return p.lease.plan.Profile(raw)
}

// Close must follow closing every perf descriptor. Any cleanup uncertainty is
// persistent even when a later caller repeats Close.
func (p *ProbeSet) Close() error {
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if p.lease != nil {
		p.closeErr = p.lease.close()
	}
	p.closeErr = errors.Join(p.closeErr, p.fs.close())
	return p.closeErr
}
