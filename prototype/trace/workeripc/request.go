package workeripc

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// NormalExitGrace bounds terminal evidence after observation expiry. It does
// not extend event delivery, authorisation or the kernel observation deadline.
const NormalExitGrace = time.Second

// Request transfers frozen admission choices, never an executable, file path,
// gadget reference or bytecode. The target directory is inherited separately.
// ManifestSHA256 must match the worker's independent installation allowlist.
type Request struct {
	Specification  trace.Specification
	IssuedAt       time.Time
	Deadline       time.Time
	ManifestSHA256 string
}

func (Request) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private worker request]") }
func (Request) MarshalJSON() ([]byte, error) { return nil, ErrProtocol }

type requestWire struct {
	Version            int              `json:"version"`
	Kind               trace.Kind       `json:"kind"`
	Paths              trace.PathPolicy `json:"paths"`
	Namespace          string           `json:"namespace"`
	PodName            string           `json:"podName"`
	PodUID             string           `json:"podUID"`
	ContainerName      string           `json:"containerName"`
	ContainerID        string           `json:"containerID"`
	ContainerStartedAt time.Time        `json:"containerStartedAt"`
	NodeUID            string           `json:"nodeUID"`
	CgroupID           uint64           `json:"cgroupID"`
	DurationNS         int64            `json:"durationNS"`
	Events             uint64           `json:"events"`
	OutputBytes        uint64           `json:"outputBytes"`
	MapBytes           uint64           `json:"mapBytes"`
	PathBytes          uint64           `json:"pathBytes"`
	IssuedAt           time.Time        `json:"issuedAt"`
	Deadline           time.Time        `json:"deadline"`
	ManifestSHA256     string           `json:"manifestSHA256"`
}

func (r Request) Validate() error {
	if r.Specification.Validate() != nil ||
		r.IssuedAt.IsZero() || !r.Deadline.After(r.IssuedAt) || r.Deadline.Sub(r.IssuedAt) > r.Specification.Bounds().Duration ||
		r.IssuedAt.Before(r.Specification.Target().ContainerStartedAt) ||
		len(r.ManifestSHA256) != 64 || strings.Trim(r.ManifestSHA256, "0123456789abcdef") != "" {
		return ErrProtocol
	}
	return nil
}

func WriteRequest(w io.Writer, r Request) error {
	if r.Validate() != nil {
		return ErrProtocol
	}
	s, t, b := r.Specification, r.Specification.Target(), r.Specification.Bounds()
	data, err := encode(requestWire{
		Version: Version, Kind: s.Kind(), Paths: s.Paths(), Namespace: t.Namespace,
		PodName: t.PodName, PodUID: t.PodUID, ContainerName: t.ContainerName,
		ContainerID: t.ContainerID, ContainerStartedAt: t.ContainerStartedAt.UTC(),
		NodeUID: t.NodeUID, CgroupID: t.CgroupID, DurationNS: int64(b.Duration),
		Events: b.Events, OutputBytes: b.OutputBytes, MapBytes: b.MapBytes, PathBytes: b.PathBytes,
		IssuedAt: r.IssuedAt.UTC(), Deadline: r.Deadline.UTC(), ManifestSHA256: r.ManifestSHA256,
	})
	if err != nil {
		return err
	}
	return send(w, data)
}

func ReadRequest(r io.Reader) (Request, error) {
	var w requestWire
	if _, err := receive(r, &w); err != nil || w.Version != Version || end(r) != nil {
		return Request{}, ErrProtocol
	}
	spec, err := trace.NewSpecification(w.Kind, trace.TargetIdentity{
		Namespace: w.Namespace, PodName: w.PodName, PodUID: w.PodUID,
		ContainerName: w.ContainerName, ContainerID: w.ContainerID,
		ContainerStartedAt: w.ContainerStartedAt, NodeUID: w.NodeUID, CgroupID: w.CgroupID,
	}, w.Paths, trace.Bounds{Duration: time.Duration(w.DurationNS), Events: w.Events, OutputBytes: w.OutputBytes, MapBytes: w.MapBytes, PathBytes: w.PathBytes})
	out := Request{spec, w.IssuedAt, w.Deadline, w.ManifestSHA256}
	if err != nil || out.Validate() != nil {
		return Request{}, ErrProtocol
	}
	return out, nil
}
