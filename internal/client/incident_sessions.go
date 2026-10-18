package client

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

type IncidentSessions interface {
	Close()
	StartIncident(context.Context) (incidentsession.Summary, error)
	InspectIncident(context.Context, string) (incidentsession.Summary, error)
	AnnotateIncident(context.Context, string, string) (incidentsession.Summary, error)
	CaptureIncident(context.Context, string, string) (incidentsession.Summary, error)
	ReferenceIncidentTrace(context.Context, string, []byte) (incidentsession.Summary, error)
	CompareIncident(context.Context, string, string, string) (incidentsession.Summary, error)
	MarkIncident(context.Context, string, string, memoryhistory.Query) (incidentsession.Summary, error)
	CloseIncident(context.Context, string) (incidentsession.Summary, error)
	DeleteIncident(context.Context, string) error
	ExportIncident(context.Context, string, incidentsession.Operation) ([]byte, error)
}

func (c *IncidentClient) action(ctx context.Context, id, action string, input any) (incidentsession.Summary, error) {
	if id == "" && action != "" {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	body, err := json.Marshal(input)
	if err != nil {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	status := http.StatusOK
	if id == "" {
		status = http.StatusCreated
	}
	data, err := c.exchange(ctx, http.MethodPost, id, action, body, status, incidentsession.MaxResourceBytes)
	if err != nil {
		return incidentsession.Summary{}, err
	}
	result, err := incidentsession.DecodeResource(data, c.namespace, id)
	if err != nil || !incidentActionAcknowledged(action, result.Latest.Kind) {
		return incidentsession.Summary{}, &IncidentError{OutcomeUnknown: true}
	}
	return result, nil
}

func (c *IncidentClient) StartIncident(ctx context.Context) (incidentsession.Summary, error) {
	return c.action(ctx, "", "", struct{}{})
}
func (c *IncidentClient) InspectIncident(ctx context.Context, id string) (incidentsession.Summary, error) {
	if !validIncidentID(id) {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	data, err := c.exchange(ctx, http.MethodGet, id, "", nil, http.StatusOK, incidentsession.MaxResourceBytes)
	if err != nil {
		return incidentsession.Summary{}, err
	}
	result, err := incidentsession.DecodeResource(data, c.namespace, id)
	if err != nil {
		return incidentsession.Summary{}, &IncidentError{}
	}
	return result, nil
}
func (c *IncidentClient) AnnotateIncident(ctx context.Context, id, note string) (incidentsession.Summary, error) {
	if incidentsession.ValidateAnnotation(note) != nil {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	return c.action(ctx, id, "entries", struct {
		Note string `json:"note"`
	}{note})
}
func (c *IncidentClient) CaptureIncident(ctx context.Context, id, pod string) (incidentsession.Summary, error) {
	return c.action(ctx, id, "capture", struct {
		Pod string `json:"pod"`
	}{pod})
}

// ReferenceIncidentTrace validates local report bytes and sends only the derived
// typed reference. Neither the server nor Kubernetes audit receives report text.
func (c *IncidentClient) ReferenceIncidentTrace(ctx context.Context, id string, data []byte) (incidentsession.Summary, error) {
	ref, err := tracereport.Describe(data)
	if err != nil {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	return c.action(ctx, id, incidentsession.TraceReferenceSubresource, incidentsession.TraceReferenceRequest{Reference: ref})
}
func (c *IncidentClient) CompareIncident(ctx context.Context, id, before, after string) (incidentsession.Summary, error) {
	return c.action(ctx, id, "compare", struct {
		Before string `json:"before"`
		After  string `json:"after"`
	}{before, after})
}
func (c *IncidentClient) MarkIncident(ctx context.Context, id, pod string, query memoryhistory.Query) (incidentsession.Summary, error) {
	return c.action(ctx, id, "markers", struct {
		Pod   string              `json:"pod"`
		Query memoryhistory.Query `json:"query"`
	}{pod, query})
}
func (c *IncidentClient) CloseIncident(ctx context.Context, id string) (incidentsession.Summary, error) {
	if !validIncidentID(id) {
		return incidentsession.Summary{}, incidentsession.ErrInvalid
	}
	return c.action(ctx, id, "close", struct{}{})
}
func (c *IncidentClient) DeleteIncident(ctx context.Context, id string) error {
	if !validIncidentID(id) {
		return incidentsession.ErrInvalid
	}
	_, err := c.exchange(ctx, http.MethodDelete, id, "", nil, http.StatusNoContent, 0)
	return err
}
func (c *IncidentClient) ExportIncident(ctx context.Context, id string, mode incidentsession.Operation) ([]byte, error) {
	if !validIncidentID(id) {
		return nil, incidentsession.ErrInvalid
	}
	action := "export"
	if mode == incidentsession.ExportAuthorised {
		action = "export-sensitive"
	} else if mode != incidentsession.ExportSanitised {
		return nil, incidentsession.ErrInvalid
	}
	data, err := c.exchange(ctx, http.MethodGet, id, action, nil, http.StatusOK, incidentsession.MaxExportBytes)
	if err != nil {
		return nil, err
	}
	doc, err := incidentsession.DecodeExport(data)
	if err != nil {
		return nil, &IncidentError{}
	}
	if mode == incidentsession.ExportSanitised && doc.Sanitised == nil {
		return nil, &IncidentError{}
	}
	if mode == incidentsession.ExportAuthorised && (doc.Authorised == nil || doc.Authorised.ID != id || doc.Authorised.Entries[0].Namespace != c.namespace) {
		return nil, &IncidentError{}
	}
	return data, nil
}

func incidentActionAcknowledged(action string, kind incidentsession.Kind) bool {
	switch action {
	case "":
		return kind == incidentsession.Opened
	case "entries":
		return kind == incidentsession.Annotated
	case "capture":
		return kind == incidentsession.Captured || kind == incidentsession.Gap
	case incidentsession.TraceReferenceSubresource:
		return kind == incidentsession.TraceReferenced
	case "markers":
		return kind == incidentsession.Marked || kind == incidentsession.Gap
	case "compare":
		return kind == incidentsession.Compared || kind == incidentsession.Gap
	case "close":
		return kind == incidentsession.Closed
	default:
		return false
	}
}
