package traceaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const MaxRecordBytes = 2048

type Operation string
type Decision string
type Reason string
type OutcomeSource string

const (
	StreamOutcome     OutcomeSource = "validated_stream"
	ControllerOutcome OutcomeSource = "controller"
)

const (
	Configure          Operation = "configure"
	Configured         Reason    = "configured"
	Preflight          Operation = "preflight"
	Create             Operation = "create"
	Read               Operation = "get"
	Attach             Operation = "stream"
	Cancel             Operation = "delete"
	Terminal           Operation = "terminal"
	Cleanup            Operation = "cleanup"
	Accepted           Decision  = "accepted"
	Rejected           Decision  = "rejected"
	Observed           Decision  = "observed"
	Checked            Reason    = "checked"
	Admitted           Reason    = "admitted"
	Revalidated        Reason    = "revalidated"
	Attached           Reason    = "attached"
	Cancelled          Reason    = "cancelled"
	Expired            Reason    = "expired"
	EventLimit         Reason    = "event_limit"
	OutputLimit        Reason    = "output_limit"
	TargetChanged      Reason    = "target_changed"
	Denied             Reason    = "denied"
	Unauthenticated    Reason    = "unauthenticated"
	Capacity           Reason    = "capacity"
	InvalidRequest     Reason    = "invalid_request"
	Unavailable        Reason    = "unavailable"
	NotFound           Reason    = "not_found"
	EngineFailed       Reason    = "engine_failed"
	Cleaned            Reason    = "cleanup_confirmed"
	CleanupUnconfirmed Reason    = "cleanup_unconfirmed"
)

// Limits includes the frozen request and the policy's concurrency ceilings.
// Results are streamed, never retained by the server after the stream closes.
type Limits struct {
	DurationNanos        int64  `json:"durationNanos"`
	Events               uint64 `json:"events"`
	OutputBytes          uint64 `json:"outputBytes"`
	MapBytes             uint64 `json:"mapBytes"`
	PathBytes            uint64 `json:"pathBytes"`
	PerActor             int    `json:"perActor"`
	PerTenant            int    `json:"perTenant"`
	PerNode              int    `json:"perNode"`
	Cluster              int    `json:"cluster"`
	PendingTTLNanos      int64  `json:"pendingTTLNanos"`
	ResultRetentionNanos int64  `json:"resultRetentionNanos"`
}

func (l Limits) validate() bool {
	bounds := trace.Bounds{Duration: time.Duration(l.DurationNanos), Events: l.Events, OutputBytes: l.OutputBytes, MapBytes: l.MapBytes, PathBytes: l.PathBytes}
	return bounds.Validate() == nil && l.PerActor >= 1 && l.PerActor <= 2 && l.PerTenant >= 1 && l.PerTenant <= 4 && l.PerNode >= 1 && l.PerNode <= 2 && l.Cluster >= 1 && l.Cluster <= 64 && l.PendingTTLNanos > 0 && l.PendingTTLNanos <= int64(15*time.Second) && l.ResultRetentionNanos == 0
}

// Event deliberately has no raw identity, error text, path or event fields.
// Only NewRecord may turn this potentially unvalidated input into log bytes.
type Event struct {
	Time          time.Time     `json:"time"`
	KeyID         string        `json:"keyID"`
	PolicyRef     string        `json:"policyRef"`
	ActorClass    string        `json:"actorClass"`
	ActorRef      string        `json:"actorRef,omitempty"`
	TenantRef     string        `json:"tenantRef,omitempty"`
	TargetRef     string        `json:"targetRef,omitempty"`
	SessionRef    string        `json:"sessionRef,omitempty"`
	Operation     Operation     `json:"operation"`
	Decision      Decision      `json:"decision"`
	Reason        Reason        `json:"reason"`
	OutcomeSource OutcomeSource `json:"outcomeSource,omitempty"`
	Kind          trace.Kind    `json:"traceKind,omitempty"`
	Limits        *Limits       `json:"limits,omitempty"`
}

func (Event) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace audit event]") }
func (Event) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

type Record struct{ data string }

func (r Record) MarshalJSON() ([]byte, error) {
	if r.data == "" {
		return nil, ErrInvalid
	}
	return []byte(r.data), nil
}

func (r Record) Bytes() ([]byte, error) {
	if r.data == "" {
		return nil, ErrInvalid
	}
	return []byte(r.data + "\n"), nil
}
func NewRecord(event Event) (Record, error) {
	if !event.valid() {
		return Record{}, ErrInvalid
	}
	event.Time = event.Time.UTC()
	type fields Event
	document := struct {
		SchemaVersion int    `json:"schemaVersion"`
		Type          string `json:"type"`
		fields
	}{1, "trace_audit", fields(event)}
	data, err := json.Marshal(document)
	if err != nil || len(data)+1 > MaxRecordBytes {
		return Record{}, ErrInvalid
	}
	return Record{data: string(data)}, nil
}
func (e Event) valid() bool {
	if e.Operation == Terminal {
		if e.OutcomeSource != StreamOutcome && e.OutcomeSource != ControllerOutcome {
			return false
		}
	} else if e.OutcomeSource != "" {
		return false
	}
	if e.Time.IsZero() || !digest(e.KeyID, "sha256:") || !digest(e.PolicyRef, "sha256:") {
		return false
	}
	for _, ref := range []string{e.ActorRef, e.TenantRef, e.TargetRef, e.SessionRef} {
		if ref != "" && !digest(ref, "hmac-sha256:") {
			return false
		}
	}
	switch e.ActorClass {
	case "user", "serviceaccount":
		if e.ActorRef == "" {
			return false
		}
	case "unauthenticated":
		if e.ActorRef != "" {
			return false
		}
	case "system":
	default:
		return false
	}
	if e.Kind != "" && e.Kind != trace.Files && e.Kind != trace.Cache && e.Kind != trace.OOM {
		return false
	}
	if e.Limits != nil && !e.Limits.validate() {
		return false
	}
	switch e.Operation {
	case Configure, Preflight, Create, Read, Attach, Cancel, Terminal, Cleanup:
	default:
		return false
	}
	switch e.Decision {
	case Accepted:
		return e.accepted()
	case Rejected:
		if e.Operation == Terminal || e.Operation == Cleanup {
			return false
		}
		switch e.Reason {
		case Unauthenticated, Denied, Capacity, InvalidRequest, Unavailable, NotFound, Expired, TargetChanged:
			return true
		}
	case Observed:
		if e.Operation == Configure {
			return e.ActorClass == "system" && e.Reason == Configured && e.SessionRef == "" && e.Limits != nil
		}
		if e.SessionRef == "" {
			return false
		}
		switch e.Operation {
		case Cleanup:
			return e.Reason == Cleaned || e.Reason == CleanupUnconfirmed
		case Terminal:
			switch e.Reason {
			case Cancelled, Expired, EventLimit, OutputLimit, TargetChanged, Denied, EngineFailed, Unavailable:
				return true
			}
		}
	}
	return false
}
func (e Event) accepted() bool {
	if e.ActorRef == "" || e.TenantRef == "" || e.Kind == "" || e.Limits == nil {
		return false
	}
	if e.Operation == Preflight {
		return e.Reason == Checked && e.SessionRef == ""
	}
	if e.TargetRef == "" || e.SessionRef == "" {
		return false
	}
	switch e.Operation {
	case Create:
		return e.Reason == Admitted
	case Read:
		return e.Reason == Revalidated
	case Attach:
		return e.Reason == Attached
	case Cancel:
		return e.Reason == Cancelled
	}
	return false
}
func digest(value, prefix string) bool {
	return len(value) == len(prefix)+64 && strings.HasPrefix(value, prefix) && strings.Trim(value[len(prefix):], "0123456789abcdef") == ""
}
