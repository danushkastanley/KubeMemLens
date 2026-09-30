package incidentsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/apimachinery/pkg/util/validation"
)

const MaxCaptureBytes = 64 << 10
const MaxCaptures = 16

// CapturedEvidence retains an exact versioned incident document. Data is base64
// on the JSON wire; its encoded size counts towards the session's total limit.
// Only an explicitly authorised export may disclose it.
type CapturedEvidence struct {
	NamespaceUID  string    `json:"namespaceUID"`
	Digest        string    `json:"digest"`
	SchemaVersion int       `json:"schemaVersion"`
	ObservedAt    time.Time `json:"observedAt"`
	Data          []byte    `json:"data"`
}

func (CapturedEvidence) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private captured evidence]")
}

func (e CapturedEvidence) reference() CaptureReference {
	return CaptureReference{NamespaceUID: e.NamespaceUID, Digest: e.Digest, SchemaVersion: e.SchemaVersion, ObservedAt: e.ObservedAt}
}

// RecordCapture is for trusted acquisition adapters, never caller-submitted
// receipts. It atomically retains bytes and appends their derived reference.
func (s *Store) RecordCapture(ctx context.Context, p Principal, id string, data []byte) (Summary, error) {
	if err := s.authorize(ctx, p, Capture, id); err != nil {
		return Summary{}, err
	}
	evidence, err := podCapture(p, data)
	if err != nil {
		return Summary{}, err
	}
	return s.retainEvidence(ctx, p, id, evidence, Captured, "collector")
}

func (s *Store) retainEvidence(ctx context.Context, p Principal, id string, evidence CapturedEvidence, kind Kind, source string, gaps ...Input) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return Summary{}, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return Summary{}, err
	}
	if r.closed != nil {
		return Summary{}, ErrClosed
	}
	next := *r
	found := false
	for _, retained := range r.captures {
		found = found || retained.Digest == evidence.Digest
	}
	if !found {
		if len(r.captures) >= MaxCaptures {
			r.limitReached = true
			return Summary{}, ErrCapacity
		}
		next.captures = append(append([]CapturedEvidence(nil), r.captures...), evidence)
	}
	input := Input{Kind: kind, Source: source, ObservedAt: evidence.ObservedAt, References: []CaptureReference{evidence.reference()}}
	for _, entry := range append([]Input{input}, gaps...) {
		if err := s.add(ctx, &next, entry, s.clock.Now()); err != nil {
			if err == ErrCapacity {
				r.limitReached = true
			}
			return Summary{}, err
		}
	}
	*r = next
	return r.summary(), nil
}

// CaptureData returns an owned copy under the same permission as full export.
// A reference without retained bytes is unavailable, never an empty capture.
func (s *Store) CaptureData(ctx context.Context, p Principal, id, digest string) ([]byte, error) {
	if err := s.authorize(ctx, p, ExportAuthorised, id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return nil, err
	}
	for _, evidence := range r.captures {
		if evidence.Digest == digest {
			return bytes.Clone(evidence.Data), nil
		}
	}
	return nil, ErrNotFound
}

func podCapture(p Principal, data []byte) (CapturedEvidence, error) {
	if len(data) == 0 || len(data) > MaxCaptureBytes || boundedCaptureData(data) != nil || !explicitPrivateCapture(data) {
		return CapturedEvidence{}, ErrInvalid
	}
	var bundle api.IncidentBundle
	if json.Unmarshal(data, &bundle, json.RejectUnknownMembers(true), json.MatchCaseInsensitiveNames(false), jsonv1.FormatDurationAsNano(true)) != nil || api.ValidateIncidentSchema(bundle) != nil {
		return CapturedEvidence{}, ErrInvalid
	}
	if bundle.CapturedAt.IsZero() || !text(bundle.ToolVersion, 512) || bundle.Redacted || len(bundle.Pods) != 1 || len(bundle.Nodes) != 0 || len(bundle.Histories) > 1 || bundle.Reliability != nil {
		return CapturedEvidence{}, ErrInvalid
	}
	pod := bundle.Pods[0]
	if pod.Namespace != p.Namespace || len(validation.IsDNS1123Subdomain(pod.PodName)) != 0 || !text(pod.PodUID, 128) || pod.CapturedAt.IsZero() || len(pod.Containers) > 128 {
		return CapturedEvidence{}, ErrInvalid
	}
	for _, c := range pod.Containers {
		if c.Namespace != pod.Namespace || c.PodName != pod.PodName || c.PodUID != pod.PodUID {
			return CapturedEvidence{}, ErrInvalid
		}
	}
	for _, h := range bundle.Histories {
		if h.Namespace != pod.Namespace || h.PodName != pod.PodName || h.PodUID != pod.PodUID {
			return CapturedEvidence{}, ErrInvalid
		}
	}
	sum := sha256.Sum256(data)
	return CapturedEvidence{NamespaceUID: p.NamespaceUID, Digest: hex.EncodeToString(sum[:]), SchemaVersion: bundle.SchemaVersion, ObservedAt: bundle.CapturedAt.UTC(), Data: bytes.Clone(data)}, nil
}

func validateCaptures(d AuthorisedExport, p Principal) error {
	if len(d.Captures) > MaxCaptures {
		return ErrInvalid
	}
	references := map[string]CaptureReference{}
	for _, entry := range d.Entries {
		for _, ref := range entry.References {
			references[ref.Digest] = ref
		}
	}
	seen := map[string]bool{}
	for _, retained := range d.Captures {
		actual, err := decodeRetainedCapture(p, retained.Data)
		ref := retained.reference()
		if err != nil || ref != actual.reference() || references[retained.Digest] != ref || seen[retained.Digest] {
			return ErrInvalid
		}
		seen[retained.Digest] = true
	}
	return nil
}

// RecordCaptureGap preserves failed acquisition under the capture permission.
func (s *Store) RecordCaptureGap(ctx context.Context, p Principal, id, reason string) (Summary, error) {
	if err := s.authorize(ctx, p, Capture, id); err != nil {
		return Summary{}, err
	}
	input := Input{Kind: Gap, Source: "collector", GapReason: reason}
	if input.validate(p) != nil {
		return Summary{}, ErrInvalid
	}
	return s.mutate(ctx, p, id, input)
}
