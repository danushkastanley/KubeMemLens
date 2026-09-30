package incidentsessionapi

import (
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"golang.org/x/time/rate"
)

const MaxRequestBytes = 4096

type Handler struct {
	ctx        context.Context
	cancel     context.CancelFunc
	authority  *Authority
	store      *incidentsession.Store
	captures   EvidenceProvider
	gate       chan struct{}
	global     *rate.Limiter
	namespaces map[string]*rate.Limiter
}

func NewHandler(ctx context.Context, authority *Authority, limits incidentsession.Limits, captures EvidenceProvider) (*Handler, error) {
	if ctx == nil || ctx.Err() != nil || authority == nil {
		return nil, incidentsession.ErrInvalid
	}
	ctx, cancel := context.WithCancel(ctx)
	store, err := incidentsession.New(ctx, limits, authority)
	if err != nil {
		cancel()
		return nil, err
	}
	h := &Handler{ctx: ctx, cancel: cancel, authority: authority, store: store, captures: captures, gate: make(chan struct{}, 2), global: rate.NewLimiter(10, 20), namespaces: map[string]*rate.Limiter{}}
	for name := range authority.namespaces {
		h.namespaces[name] = rate.NewLimiter(2, 8)
	}
	return h, nil
}

func (h *Handler) Shutdown() { h.cancel(); h.store.Shutdown() }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set(SchemaHeader, "1")
	target, err := requestRoute(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if h.ctx.Err() != nil {
		writeError(w, incidentsession.ErrStopped)
		return
	}
	limiter := h.namespaces[target.namespace]
	if limiter == nil {
		writeError(w, incidentsession.ErrNotFound)
		return
	}
	if !h.global.Allow() || !limiter.Allow() {
		writeError(w, incidentsession.ErrCapacity)
		return
	}
	select {
	case h.gate <- struct{}{}:
		defer func() { <-h.gate }()
	default:
		writeError(w, incidentsession.ErrCapacity)
		return
	}
	if values := r.Header.Values(SchemaHeader); len(values) != 1 || values[0] != "1" {
		writeError(w, incidentsession.ErrInvalid)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	stop := context.AfterFunc(h.ctx, cancel)
	defer func() { stop(); cancel() }()
	// Context expiry alone does not interrupt a blocked network body read.
	// Require transport deadlines so slow clients cannot retain admission slots.
	deadline, _ := ctx.Deadline()
	transport := http.NewResponseController(w)
	if transport.SetReadDeadline(deadline) != nil || transport.SetWriteDeadline(deadline) != nil {
		writeError(w, incidentsession.ErrUnavailable)
		return
	}
	p, err := h.authority.Resolve(ctx, target.namespace, target.operation, target.id)
	if err != nil {
		writeError(w, err)
		return
	}
	var summary incidentsession.Summary
	status := http.StatusOK
	switch target.operation {
	case incidentsession.Create:
		if readBody(r, &struct{}{}) != nil {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		summary, err = h.store.Start(ctx, p)
		status = http.StatusCreated
	case incidentsession.Markers:
		summary, err = h.markers(ctx, p, target.id, r)
	case incidentsession.Compare:
		var input struct {
			Before string `json:"before"`
			After  string `json:"after"`
		}
		if readBody(r, &input) != nil {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		summary, err = h.store.CompareCaptures(ctx, p, target.id, input.Before, input.After)
	case incidentsession.Capture:
		summary, err = h.capture(ctx, p, target.id, r)
	case incidentsession.ReferenceTrace:
		var input incidentsession.TraceReferenceRequest
		if readBody(r, &input) != nil {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		summary, err = h.store.RecordTraceReference(ctx, p, target.id, input.Reference)
	case incidentsession.Append:
		var input struct {
			Note string `json:"note"`
		}
		if readBody(r, &input) != nil {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		summary, err = h.store.Append(ctx, p, target.id, incidentsession.Input{Kind: incidentsession.Annotated, Source: "operator", Note: input.Note})
	case incidentsession.CloseSession:
		if readBody(r, &struct{}{}) != nil {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		summary, err = h.store.Close(ctx, p, target.id)
	default:
		if !emptyBody(r) {
			writeError(w, incidentsession.ErrInvalid)
			return
		}
		switch target.operation {
		case incidentsession.Inspect:
			summary, err = h.store.Inspect(ctx, p, target.id)
		case incidentsession.Delete:
			err = h.store.Delete(ctx, p, target.id)
			if err != nil {
				writeError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case incidentsession.ExportSanitised, incidentsession.ExportAuthorised:
			var data []byte
			data, err = h.store.Export(ctx, p, target.id, target.operation)
			if err != nil {
				writeError(w, err)
				return
			}
			writeBytes(w, http.StatusOK, data)
			return
		}
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeSummary(w, status, target.namespace, summary)
}

func readBody(r *http.Request, target any) error {
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" || len(params) > 1 {
		return incidentsession.ErrInvalid
	}
	for name, value := range params {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return incidentsession.ErrInvalid
		}
	}
	if r.Body == nil {
		return incidentsession.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
	if err != nil || len(data) > MaxRequestBytes {
		return incidentsession.ErrInvalid
	}
	if json.Unmarshal(data, target, json.RejectUnknownMembers(true), json.MatchCaseInsensitiveNames(false), jsonv1.FormatDurationAsNano(true)) != nil {
		return incidentsession.ErrInvalid
	}
	// A JSON null must not stand in for an explicit empty create/close object.
	if len(strings.TrimSpace(string(data))) == 0 || strings.TrimSpace(string(data))[0] != '{' {
		return incidentsession.ErrInvalid
	}
	return nil
}

func emptyBody(r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 1))
	return err == nil && len(data) == 0
}
