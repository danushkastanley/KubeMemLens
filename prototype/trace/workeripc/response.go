package workeripc

import (
	"encoding/json"
	"math/bits"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

type responseWire struct {
	Version int         `json:"version"`
	Type    string      `json:"type"`
	File    *fileWire   `json:"file,omitempty"`
	Cache   *cacheWire  `json:"cache,omitempty"`
	Result  *resultWire `json:"result,omitempty"`
}
type fileWire struct {
	ObservedAt time.Time           `json:"observedAt"`
	Operation  trace.FileOperation `json:"operation"`
	Requested  uint64              `json:"requested"`
	Completed  uint64              `json:"completed"`
	Path       string              `json:"path"`
}
type cacheWire struct {
	ObservedAt time.Time            `json:"observedAt"`
	Operation  trace.CacheOperation `json:"operation"`
	Pages      uint64               `json:"pages"`
}
type resultWire struct {
	StartedAt   time.Time         `json:"startedAt"`
	EndedAt     time.Time         `json:"endedAt"`
	Termination trace.Termination `json:"termination"`
	Produced    *uint64           `json:"produced"`
	Sampled     *uint64           `json:"sampled"`
	Lost        *uint64           `json:"lost"`
	Rejected    *uint64           `json:"rejected"`
	Incomplete  bool              `json:"incomplete"`
	Correlation json.RawMessage   `json:"correlation,omitempty"`
}

func observed(at time.Time, r Request) bool {
	return !at.IsZero() && !at.Before(r.IssuedAt.Add(-5*time.Millisecond)) && !at.After(r.Deadline.Add(5*time.Millisecond))
}

func (w responseWire) validate(r Request) error {
	if w.Version != Version {
		return ErrProtocol
	}
	switch w.Type {
	case "ready":
		if w.File != nil || w.Cache != nil || w.Result != nil {
			return ErrProtocol
		}
	case "file":
		if w.File == nil || w.Cache != nil || w.Result != nil || r.Specification.Kind() != trace.Files {
			return ErrProtocol
		}
		f := w.File
		if !observed(f.ObservedAt, r) || (f.Operation != trace.FileRead && f.Operation != trace.FileWrite) || f.Completed > f.Requested ||
			uint64(len(f.Path)) > r.Specification.Bounds().PathBytes || !utf8.ValidString(f.Path) {
			return ErrProtocol
		}
		for _, c := range f.Path {
			if c == 0 {
				return ErrProtocol
			}
		}
		if (r.Specification.Paths() == trace.OmitPaths && f.Path != "") || (r.Specification.Paths() == trace.ConfirmedPaths && f.Path == "") {
			return ErrProtocol
		}
	case "cache":
		if w.Cache == nil || w.File != nil || w.Result != nil || r.Specification.Kind() != trace.Cache {
			return ErrProtocol
		}
		c := w.Cache
		if !observed(c.ObservedAt, r) || (c.Operation != trace.CacheAdd && c.Operation != trace.CacheRemove) || c.Pages == 0 || c.Pages > 1<<62 || c.Pages&(c.Pages-1) != 0 {
			return ErrProtocol
		}
	case "result":
		if w.Result == nil || w.File != nil || w.Cache != nil {
			return ErrProtocol
		}
		return w.Result.validate(r)
	default:
		return ErrProtocol
	}
	return nil
}

func (w resultWire) validate(r Request) error {
	switch w.Termination {
	case trace.Expired, trace.Cancelled, trace.TargetChanged, trace.OutputLimit, trace.EventLimit, trace.EngineFailed, trace.AuthorisationLost:
	default:
		return ErrProtocol
	}
	// Known hook blind spots make every current file/cache result incomplete.
	if !w.Incomplete {
		return ErrProtocol
	}
	if w.StartedAt.IsZero() != w.EndedAt.IsZero() {
		return ErrProtocol
	}
	if !w.StartedAt.IsZero() && (!observed(w.StartedAt, r) || !observed(w.EndedAt, r) || w.EndedAt.Before(w.StartedAt)) {
		return ErrProtocol
	}
	if w.Termination == trace.AuthorisationLost && len(w.Correlation) != 0 {
		return ErrProtocol
	}
	correlation, err := traceevidence.Decode(w.Correlation, w.StartedAt, w.EndedAt, r.Specification.Bounds().Duration)
	if err != nil || (correlation != nil && correlation.State == "overlapping" && (correlation.EvidenceStart.Before(r.IssuedAt) || correlation.EvidenceEnd.After(r.Deadline.Add(NormalExitGrace)))) {
		return ErrProtocol
	}
	counts := []*uint64{w.Produced, w.Sampled, w.Lost, w.Rejected}
	for _, count := range counts {
		if (count == nil) != (w.Produced == nil) {
			return ErrProtocol
		}
	}
	if w.Produced == nil {
		return nil
	}
	total, carry := bits.Add64(*w.Sampled, *w.Lost, 0)
	if carry != 0 {
		return ErrProtocol
	}
	total, carry = bits.Add64(total, *w.Rejected, 0)
	if carry != 0 || total > *w.Produced {
		return ErrProtocol
	}
	return nil
}

func (w resultWire) result(r Request) (trace.Result, error) {
	correlation, err := traceevidence.Decode(w.Correlation, w.StartedAt, w.EndedAt, r.Specification.Bounds().Duration)
	if err != nil {
		return trace.Result{}, ErrProtocol
	}
	return trace.Result{Version: trace.ContractVersion, StartedAt: w.StartedAt, EndedAt: w.EndedAt, Termination: w.Termination,
		Counts: trace.Counts{Produced: w.Produced, Sampled: w.Sampled, Lost: w.Lost, Rejected: w.Rejected}, Incomplete: w.Incomplete, Correlation: correlation}, nil
}

// A valid cumulative result cannot account for fewer successful pipe events
// than were already delivered. An unknown counter set remains unknown.
func (w resultWire) covers(events uint64) bool {
	return w.Produced == nil || *w.Produced-*w.Sampled-*w.Lost-*w.Rejected >= events
}
