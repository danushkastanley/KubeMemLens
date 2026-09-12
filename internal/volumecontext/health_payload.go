package volumecontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

const MaxHealthPayloadBytes = 32 << 10

// HealthPayload is immutable, sanitised status for bounded internal retention.
// Observation time and identity are separate from the deduplicated payload.
type HealthPayload struct{ data []byte }

type healthPayloadWire struct {
	Source        volumehealth.Source       `json:"source"`
	Scope         volumehealth.Scope        `json:"scope"`
	Availability  volumehealth.Availability `json:"availability"`
	Reason        volumehealth.Reason       `json:"reason,omitempty"`
	TransitionAt  time.Time                 `json:"transitionAt,omitzero"`
	Conditions    []Condition               `json:"conditions"`
	TextTruncated bool                      `json:"textTruncated"`
}

func NewHealthPayload(input volumehealth.Observation, now time.Time) (HealthPayload, error) {
	o, err := SanitiseHealth(input, now)
	if err != nil {
		return HealthPayload{}, err
	}
	h := namedHealth(o)
	w := healthPayloadWire{Source: o.Source, Scope: o.Scope, Availability: o.Availability, Reason: o.Reason, TransitionAt: o.TransitionAt, Conditions: h.Conditions, TextTruncated: o.TextTruncated}
	data, err := json.Marshal(w)
	if err != nil || len(data) > MaxHealthPayloadBytes {
		return HealthPayload{}, ErrInvalid
	}
	return HealthPayload{data: data}, nil
}

func (p HealthPayload) Size() int                      { return len(p.data) }
func (p HealthPayload) Equal(other HealthPayload) bool { return bytes.Equal(p.data, other.data) }

func (p HealthPayload) Observation(at time.Time) (volumehealth.Observation, error) {
	var w healthPayloadWire
	if len(p.data) == 0 || len(p.data) > MaxHealthPayloadBytes || json.Unmarshal(p.data, &w) != nil {
		return volumehealth.Observation{}, ErrInvalid
	}
	o := volumehealth.Observation{Source: w.Source, Scope: w.Scope, Availability: w.Availability, Reason: w.Reason, ObservedAt: at, TransitionAt: w.TransitionAt, TextTruncated: w.TextTruncated}
	for _, c := range w.Conditions {
		o.Conditions = append(o.Conditions, volumehealth.Condition{Status: c.Status, Reason: c.Reason, TransitionAt: c.TransitionAt, AccessMode: c.AccessMode, VolumeMode: c.VolumeMode})
	}
	return SanitiseHealth(o, at)
}

func (HealthPayload) String() string                   { return "retained health status" }
func (p HealthPayload) GoString() string               { return p.String() }
func (HealthPayload) MarshalJSON() ([]byte, error)     { return []byte(`{"retained":true}`), nil }
func (p HealthPayload) Format(state fmt.State, _ rune) { _, _ = fmt.Fprint(state, p.String()) }
