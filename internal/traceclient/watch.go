package traceclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type Result struct {
	ContractVersion        tracecompat.Version
	StreamVersion          int
	Metadata               traceframe.ClientMetadata
	Bytes, DeliveredEvents uint64
	TransportComplete      bool
	summary                traceframe.Frame
}

func (Result) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace result]") }
func (Result) MarshalJSON() ([]byte, error) { return nil, failure(Invalid) }
func (r Result) Summary() (traceframe.Summary, bool) {
	s, err := r.summary.ClientSummary()
	return s, err == nil
}

// Watch consumes one ephemeral stream, retaining metadata and one validated
// summary only. observe must return promptly; event payloads are never retained
// or passed to it. Updates are coalesced to at most ten per second while reading.
// Use Cancel on the separate control connection for confirmed cancellation.
func (c *Client) Watch(ctx context.Context, a Admission, observe func(Result)) (result Result, resultErr error) {
	if c == nil || a.client != c {
		return result, failure(Invalid)
	}
	path, err := admissionPath(a.namespace, a.id)
	if err != nil {
		return result, err
	}
	select {
	case c.streamSlot <- struct{}{}:
		defer func() { <-c.streamSlot }()
	default:
		return result, failure(Capacity)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute+2*time.Second)
	defer cancel()
	timer := time.AfterFunc(controlTimeout, cancel)
	defer timer.Stop()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"/stream?"+tracecompat.StreamQuery, nil)
	if err != nil {
		return result, failure(Invalid)
	}
	r.Header.Set("Accept", "application/x-ndjson")
	r.Header.Set("Cache-Control", "no-store")
	r.Header.Set(tracecompat.Header, tracecompat.Offer)
	// This dedicated HTTP/1 transport serves streams only. Closing every
	// connection prevents a later activation GET from using a stale connection
	// that net/http could transparently replay. Native TLS rotation is retained.
	r.Close = true
	response, err := c.stream.Do(r)
	if err != nil {
		return result, failure(Incomplete)
	}
	defer response.Body.Close()
	if !boundedHeaders(response.Header) {
		return result, failure(Protocol)
	}
	if response.StatusCode != 200 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return result, contractStatusError(response.StatusCode, response.Header)
	}
	if tracecompat.AcceptResponse(response.Header.Values(tracecompat.Header)) != nil {
		return result, failure(Incompatible)
	}
	result.ContractVersion = tracecompat.Current
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/x-ndjson" {
		return result, failure(Protocol)
	}
	reader := traceframe.NewReader(http.MaxBytesReader(nil, response.Body, 32<<20))
	first, err := reader.Next()
	if err != nil {
		return result, streamError(err)
	}
	metadata, err := first.ClientMetadata()
	if err != nil || metadata.SessionID != a.id || metadata.Namespace != a.namespace || metadata.EngineDigest != a.engine {
		return result, failure(Protocol)
	}
	if a.plan != nil {
		p := a.plan
		s := p.selection
		expected := traceframe.ClientClaim{SessionID: a.id, EngineDigest: a.engine, ProgrammeDigest: p.document.Node.ProgrammeDigest,
			Namespace: s.Namespace, Pod: s.Pod, PodUID: s.PodUID, Container: s.Container, ContainerStartedAt: s.ContainerStartedAt, Kind: p.intent.Kind, Paths: p.intent.Paths, Bounds: p.intent.Bounds}
		if first.Version() != p.document.Node.StreamVersion {
			return result, failure(Protocol)
		}
		metadata, err = first.MatchClientClaim(expected)
		if err != nil {
			return result, failure(TargetChanged)
		}
	}
	if ctx.Err() != nil {
		return result, failure(Incomplete)
	}
	timer.Reset(metadata.Bounds.Duration + 2*time.Second)
	result.Metadata = metadata
	result.StreamVersion = first.Version()
	result.Bytes, result.DeliveredEvents = reader.Counts()
	if observe != nil {
		observe(result)
	}
	lastUpdate := time.Now()
	for {
		frame, err := reader.Next()
		result.Bytes, result.DeliveredEvents = reader.Counts()
		if err == io.EOF {
			result.TransportComplete = true
			if observe != nil {
				observe(result)
			}
			return result, nil
		}
		if err != nil {
			if errors.Is(err, traceframe.ErrInvalid) {
				result.summary = traceframe.Frame{}
			}
			return result, streamError(err)
		}
		if frame.Type() == traceframe.SummaryFrame {
			result.summary = frame
			timer.Reset(2 * time.Second) // Require EOF; trailing frames are not accepted.
		}
		if observe != nil && time.Since(lastUpdate) >= 100*time.Millisecond {
			observe(result)
			lastUpdate = time.Now()
		}
	}
}
func streamError(err error) error {
	if errors.Is(err, traceframe.ErrInvalid) {
		return failure(Protocol)
	}
	return failure(Incomplete)
}
