package trace

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type Kind string

const (
	Files Kind = "files"
	Cache Kind = "cache"
	OOM   Kind = "oom"
)

type PathPolicy string

const (
	OmitPaths      PathPolicy = "omit"
	ConfirmedPaths PathPolicy = "confirmed"
)

// TargetIdentity must be resolved by the authenticated server and verified
// against a live node-side cgroup handle. Syntax validation is not admission.
type TargetIdentity struct {
	Namespace          string
	PodName            string
	PodUID             string
	ContainerName      string
	ContainerID        string
	ContainerStartedAt time.Time
	NodeUID            string
	CgroupID           uint64
}

// Target identities must not leak through ordinary structured or formatted logs.
func (TargetIdentity) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace target]") }
func (TargetIdentity) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace target requires an explicit private transport")
}

type Bounds struct {
	Duration    time.Duration
	Events      uint64
	OutputBytes uint64
	MapBytes    uint64
	PathBytes   uint64
}

func DefaultBounds() Bounds {
	return Bounds{Duration: 30 * time.Second, Events: 10_000, OutputBytes: 8 << 20, MapBytes: 8 << 20, PathBytes: 256}
}

// Specification is a value owned by admission, not a client request DTO.
// Construction validates the fixed engine vocabulary and absolute ceilings;
// admission must additionally enforce caller policy, consent and current identity.
type Specification struct {
	kind   Kind
	target TargetIdentity
	paths  PathPolicy
	bounds Bounds
}

func NewSpecification(kind Kind, target TargetIdentity, paths PathPolicy, bounds Bounds) (Specification, error) {
	switch kind {
	case Files, Cache, OOM:
	default:
		return Specification{}, errors.New("unsupported trace kind")
	}
	if paths != OmitPaths && paths != ConfirmedPaths {
		return Specification{}, errors.New("invalid trace path policy")
	}
	if paths == ConfirmedPaths && kind != Files {
		return Specification{}, errors.New("raw paths require a file trace")
	}
	if err := validateTarget(target); err != nil {
		return Specification{}, err
	}
	if err := validateBounds(bounds); err != nil {
		return Specification{}, err
	}
	target.ContainerStartedAt = target.ContainerStartedAt.UTC()
	return Specification{kind: kind, target: target, paths: paths, bounds: bounds}, nil
}

func (s Specification) Kind() Kind             { return s.kind }
func (s Specification) Target() TargetIdentity { return s.target }
func (s Specification) Paths() PathPolicy      { return s.paths }
func (s Specification) Bounds() Bounds         { return s.bounds }

// Validate rejects the zero value at an engine boundary as well as any future
// invalid construction. The current server policy may impose lower ceilings.
func (s Specification) Validate() error {
	_, err := NewSpecification(s.kind, s.target, s.paths, s.bounds)
	return err
}

func (Specification) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace specification]") }
func (Specification) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace specification requires an explicit private transport")
}

func validateTarget(t TargetIdentity) error {
	for _, value := range []string{t.Namespace, t.PodName, t.PodUID, t.ContainerName, t.NodeUID} {
		if value == "" || len(value) > 253 || strings.ContainsAny(value, "\x00\r\n\t /\\") {
			return errors.New("invalid trace target identity")
		}
	}
	if len(t.PodUID) > 128 || len(t.NodeUID) > 128 {
		return errors.New("invalid trace target UID")
	}
	// The initial containerd profile requires the exact full runtime identifier.
	// Diagnostic prefix matching must never be reused for tracing authority.
	if len(t.ContainerID) != 64 || strings.Trim(t.ContainerID, "0123456789abcdef") != "" {
		return errors.New("trace target requires a full container identifier")
	}
	if t.ContainerStartedAt.IsZero() || t.CgroupID == 0 {
		return errors.New("trace target requires a container lifetime and cgroup binding")
	}
	return nil
}

func validateBounds(b Bounds) error {
	if b.Duration <= 0 || b.Duration > 5*time.Minute {
		return errors.New("trace duration exceeds policy")
	}
	if b.Events == 0 || b.Events > 100_000 {
		return errors.New("trace event limit exceeds policy")
	}
	if b.OutputBytes == 0 || b.OutputBytes > 32<<20 || b.MapBytes == 0 || b.MapBytes > 32<<20 {
		return errors.New("trace byte limit exceeds policy")
	}
	if b.PathBytes == 0 || b.PathBytes > 512 {
		return errors.New("trace path limit exceeds policy")
	}
	return nil
}
