package incidentsession

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

const SchemaHeader = "X-KubeMemLens-Incident-Schema"
const MaxResourceBytes = 4096
const TraceReferenceSubresource = "trace-references"

type TraceReferenceRequest struct {
	Reference tracereport.Reference `json:"reference"`
}

type Resource struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	SchemaVersion int     `json:"schemaVersion"`
	Session       Summary `json:"session"`
}

// DecodeResource binds a small action/status response to the selected namespace
// and session. An empty expected ID is used only for explicit session creation.
func DecodeResource(data []byte, namespace, id string) (Summary, error) {
	if len(data) == 0 || len(data) > MaxResourceBytes {
		return Summary{}, ErrInvalid
	}
	var resource Resource
	if jsonv2.Unmarshal(data, &resource, jsonv2.RejectUnknownMembers(true), jsonv2.MatchCaseInsensitiveNames(false)) != nil {
		return Summary{}, ErrInvalid
	}
	if resource.Kind != "IncidentSession" || resource.APIVersion != api.MemoryAPIGroup+"/"+api.MemoryAPIVersion || resource.SchemaVersion != SchemaVersion || resource.Metadata.Namespace != namespace || resource.Metadata.Name != resource.Session.ID || !digest(resource.Session.ID, 16) || (id != "" && resource.Session.ID != id) {
		return Summary{}, ErrInvalid
	}
	var root map[string]json.RawMessage
	if jsonv2.Unmarshal(data, &root) != nil {
		return Summary{}, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if jsonv2.Unmarshal(root["session"], &fields) != nil {
		return Summary{}, ErrInvalid
	}
	for _, name := range []string{"id", "openedAt", "expiresAt", "entries", "limitReached", "latest"} {
		if _, ok := fields[name]; !ok || string(fields[name]) == "null" {
			return Summary{}, ErrInvalid
		}
	}
	var latest map[string]json.RawMessage
	if jsonv2.Unmarshal(fields["latest"], &latest) != nil {
		return Summary{}, ErrInvalid
	}
	for _, name := range []string{"sequence", "kind", "source", "clockUncertain"} {
		if _, ok := latest[name]; !ok || string(latest[name]) == "null" {
			return Summary{}, ErrInvalid
		}
	}
	s := resource.Session
	retention := s.ExpiresAt.Sub(s.OpenedAt)
	if s.OpenedAt.IsZero() || retention < time.Minute || retention > 4*time.Hour || s.Entries < 1 || s.Entries > MaxEntries || s.Latest.Sequence != uint64(s.Entries) || !validSource(s.Latest.Source) {
		return Summary{}, ErrInvalid
	}
	if (s.Entries == 1) != (s.Latest.Kind == Opened) || (s.Latest.GapReason == "clock-uncertain" && !s.Latest.ClockUncertain) {
		return Summary{}, ErrInvalid
	}
	switch s.Latest.Kind {
	case Opened:
		if s.Entries != 1 || s.Latest.Source != "operator" {
			return Summary{}, ErrInvalid
		}
	case Annotated, Compared:
		if s.Latest.Source != "operator" {
			return Summary{}, ErrInvalid
		}
	case TraceReferenced:
		if s.Latest.Source != traceReferenceSource {
			return Summary{}, ErrInvalid
		}
	case Captured, Marked:
	case Gap:
		if !validGap(s.Latest.GapReason) {
			return Summary{}, ErrInvalid
		}
	case Closed:
		if s.Latest.Source != "operator" {
			return Summary{}, ErrInvalid
		}
	default:
		return Summary{}, ErrInvalid
	}
	if (s.Latest.Kind != Gap && s.Latest.GapReason != "") || (s.ClosedAt != nil) != (s.Latest.Kind == Closed) || (s.ClosedAt != nil && s.ClosedAt.IsZero()) || (s.ClosedAt == nil && s.Entries >= MaxEntries) {
		return Summary{}, ErrInvalid
	}
	return s, nil
}
