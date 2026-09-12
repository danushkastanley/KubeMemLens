// Package volumehealth represents source-separated CSI health evidence. It does
// not infer driver support, probe freshness or health from absent API fields.
package volumehealth

import (
	"context"
	"time"
)

type Source string

const (
	PodSource        Source = "pod-node-plugin"
	ControllerSource Source = "pvc-controller-plugin"
	BackendSource    Source = "csi-node-backend"
)

type Scope string

const (
	VolumeScope  Scope = "volume"
	BackendScope Scope = "node-backend"
)

type Availability string

const (
	Reported    Availability = "reported"
	Unreported  Availability = "unreported"
	Disabled    Availability = "disabled"
	Unsupported Availability = "driver-unsupported"
	Forbidden   Availability = "forbidden"
	Unavailable Availability = "unavailable"
	Unknown     Availability = "unknown"
)

type State string

const (
	StateHealthy State = "healthy"
	StateAdverse State = "adverse"
	StateStale   State = "stale"
	StateUnknown State = "unknown"
)

type Freshness string

const (
	Fresh            Freshness = "fresh"
	Stale            Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

type Status string
type Reason string

const (
	NoReport           Reason = "no-report"
	AccessDenied       Reason = "access-denied"
	ReadFailed         Reason = "read-failed"
	InvalidResponse    Reason = "invalid-response"
	BindingUnavailable Reason = "binding-unavailable"
	NotCSIVolume       Reason = "not-csi-volume"
	NotScheduled       Reason = "not-scheduled"
)

// Identity and condition text are only for callers already authorised to read
// the underlying objects. They are never included by the default JSON encoder.
type Identity struct {
	Namespace  string `json:"-"`
	PodName    string `json:"-"`
	PodUID     string `json:"-"`
	VolumeName string `json:"-"`
	PVCName    string `json:"-"`
	PVCUID     string `json:"-"`
	NodeName   string `json:"-"`
	Driver     string `json:"-"`
}

type Condition struct {
	Status       Status    `json:"-"`
	Reason       string    `json:"-"`
	Message      string    `json:"-"`
	AccessMode   string    `json:"-"`
	VolumeMode   string    `json:"-"`
	TransitionAt time.Time `json:"-"`
}

type Observation struct {
	Identity             Identity     `json:"-"`
	Source               Source       `json:"source"`
	Scope                Scope        `json:"scope"`
	Availability         Availability `json:"availability"`
	Reason               Reason       `json:"reason,omitempty"`
	State                State        `json:"state"`
	Adverse              bool         `json:"adverse"`
	UnknownStatus        bool         `json:"unknownStatus"`
	ObservationFreshness Freshness    `json:"observationFreshness"`
	ProbeFreshness       Freshness    `json:"probeFreshness"`
	ObservedAt           time.Time    `json:"observedAt"`
	TransitionAt         time.Time    `json:"-"`
	Conditions           []Condition  `json:"-"`
	TextTruncated        bool         `json:"textTruncated"`
}

// SourceReader is opt-in and bound to one caller and namespace. Query always
// authorises the current Pod before exposing any names or joining other objects.
type SourceReader interface {
	Query(context.Context, string) (Report, error)
}
