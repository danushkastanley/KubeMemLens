package tui

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/sessionview"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

type sessionOperation int

const (
	sessionStart sessionOperation = iota
	sessionRefresh
	sessionAnnotate
	sessionCapturePod
	sessionCompareEvidence
	sessionMarkers
	sessionClose
	sessionRemove
	sessionSavePublic
	sessionSavePrivate
	sessionReferenceTrace
)

type sessionRequest struct {
	operation                 sessionOperation
	namespace, id, pod, input string
	source                    memoryhistory.Source
	traceDocument             tracereport.Document
}

func (sessionRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private incident session request]")
}

func runSessionWork(ctx context.Context, connection client.IncidentSessions, request sessionRequest) sessionPanelMsg {
	result := sessionPanelMsg{operation: request.operation}
	if ctx.Err() != nil {
		result.err = ctx.Err()
		return result
	}
	switch request.operation {
	case sessionStart:
		result.summary, result.err = connection.StartIncident(ctx)
	case sessionRefresh:
		var data []byte
		data, result.err = connection.ExportIncident(ctx, request.id, incidentsession.ExportSanitised)
		if result.err == nil {
			doc, err := incidentsession.DecodeExport(data)
			result.err = err
			if err == nil && doc.Sanitised != nil {
				d := doc.Sanitised
				e := d.Entries[len(d.Entries)-1]
				result.summary = incidentsession.Summary{ID: request.id, OpenedAt: d.OpenedAt, ExpiresAt: d.ExpiresAt, ClosedAt: d.ClosedAt, Entries: len(d.Entries), LimitReached: d.LimitReached, Latest: incidentsession.EntryStatus{Sequence: e.Sequence, Kind: e.Kind, Source: e.Source, GapReason: e.GapReason, ClockUncertain: e.ClockUncertain}}
				result.lines = sessionview.SanitisedLines(*d)
			} else if err == nil {
				result.err = incidentsession.ErrInvalid
			}
		}
	case sessionAnnotate:
		result.summary, result.err = connection.AnnotateIncident(ctx, request.id, request.input)
	case sessionCapturePod:
		result.summary, result.err = connection.CaptureIncident(ctx, request.id, request.pod)
	case sessionReferenceTrace:
		result.summary, result.err = saveSessionTraceReference(ctx, connection, request)
		if result.err == nil {
			result.lines = []string{"Trace reference attached. Keep the saved local report for verification.", "The collector received only the typed reference; source authenticity is unverified."}
		}
	case sessionCompareEvidence:
		return runSessionComparison(ctx, connection, request)
	case sessionMarkers:
		query, err := memoryhistory.ParseQuery(url.Values{"source": {string(request.source)}}, memoryhistory.Pod, time.Now().UTC())
		result.err = err
		if err == nil {
			result.summary, result.err = connection.MarkIncident(ctx, request.id, request.pod, query)
		}
	case sessionClose:
		result.summary, result.err = connection.CloseIncident(ctx, request.id)
	case sessionRemove:
		result.err = connection.DeleteIncident(ctx, request.id)
		if result.err == nil {
			result.lines = []string{"Session and retained evidence deleted."}
		}
	case sessionSavePublic, sessionSavePrivate:
		result.err = saveSession(ctx, connection, request)
		if result.err == nil {
			result.lines = []string{"Session export written with mode 0600.", "Export files persist until you delete them."}
		}
	default:
		result.err = incidentsession.ErrInvalid
	}
	return result
}

func saveSession(ctx context.Context, connection client.IncidentSessions, request sessionRequest) error {
	path := strings.TrimSpace(request.input)
	if path == "" || path == "-" {
		return fmt.Errorf("enter a named destination file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid destination path")
	}
	mode := incidentsession.ExportSanitised
	if request.operation == sessionSavePrivate {
		mode = incidentsession.ExportAuthorised
	}
	data, err := connection.ExportIncident(ctx, request.id, mode)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return incident.WriteSession(io.Discard, absolute, false, data)
}
