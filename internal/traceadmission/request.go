// Package traceadmission owns exact-target trace requests and admission policy.
// It never loads a programme or accepts a caller-supplied runtime identity.
package traceadmission

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"k8s.io/apimachinery/pkg/util/validation"
)

const MaxRequestBytes = 4096

var ErrInvalidRequest = errors.New("invalid trace request")

// Request contains only user-selectable intent. Namespace comes from the
// authenticated API route; runtime identity is resolved separately by admission.
type Request struct {
	namespace string
	pod       string
	container string
	kind      trace.Kind
	paths     trace.PathPolicy
	bounds    trace.Bounds
}

func (r Request) Namespace() string       { return r.namespace }
func (r Request) Pod() string             { return r.pod }
func (r Request) Container() string       { return r.container }
func (r Request) Kind() trace.Kind        { return r.kind }
func (r Request) Paths() trace.PathPolicy { return r.paths }
func (r Request) Bounds() trace.Bounds    { return r.bounds }

func (Request) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace request]") }
func (Request) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace request requires an explicit authorised representation")
}

type requestBody struct {
	SchemaVersion   int        `json:"schemaVersion"`
	Pod             string     `json:"pod"`
	Container       string     `json:"container"`
	Kind            trace.Kind `json:"kind"`
	RawPaths        bool       `json:"rawPaths"`
	DurationSeconds *uint64    `json:"durationSeconds"`
	MaxEvents       *uint64    `json:"maxEvents"`
	MaxOutputBytes  *uint64    `json:"maxOutputBytes"`
	MaxMapBytes     *uint64    `json:"maxMapBytes"`
	MaxPathBytes    *uint64    `json:"maxPathBytes"`
}

// DecodeRequest accepts one flat, bounded, versioned object. In addition to
// unknown fields, duplicate names and null values are rejected to prevent
// intermediaries and admission from interpreting the same request differently.
func DecodeRequest(namespace string, reader io.Reader) (Request, error) {
	if !validNamespace(namespace) || reader == nil {
		return Request{}, ErrInvalidRequest
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxRequestBytes+1))
	if err != nil || len(data) > MaxRequestBytes || !utf8.Valid(data) {
		return Request{}, ErrInvalidRequest
	}
	if err := uniqueObject(data); err != nil {
		return Request{}, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var body requestBody
	if err := decoder.Decode(&body); err != nil {
		return Request{}, ErrInvalidRequest
	}
	if body.SchemaVersion != 1 || len(validation.IsDNS1123Subdomain(body.Pod)) != 0 || len(validation.IsDNS1123Label(body.Container)) != 0 {
		return Request{}, ErrInvalidRequest
	}
	switch body.Kind {
	case trace.Files, trace.Cache, trace.OOM:
	default:
		return Request{}, ErrInvalidRequest
	}
	paths := trace.OmitPaths
	if body.RawPaths {
		if body.Kind != trace.Files {
			return Request{}, ErrInvalidRequest
		}
		paths = trace.ConfirmedPaths
	}
	bounds := trace.DefaultBounds()
	if body.DurationSeconds != nil {
		// Reject before conversion so a huge unsigned duration cannot wrap.
		if *body.DurationSeconds == 0 || *body.DurationSeconds > 300 {
			return Request{}, ErrInvalidRequest
		}
		bounds.Duration = time.Duration(*body.DurationSeconds) * time.Second
	}
	setBound(&bounds.Events, body.MaxEvents)
	setBound(&bounds.OutputBytes, body.MaxOutputBytes)
	setBound(&bounds.MapBytes, body.MaxMapBytes)
	setBound(&bounds.PathBytes, body.MaxPathBytes)
	if err := bounds.Validate(); err != nil {
		return Request{}, ErrInvalidRequest
	}
	return Request{namespace: namespace, pod: body.Pod, container: body.Container, kind: body.Kind, paths: paths, bounds: bounds}, nil
}

func setBound(target *uint64, value *uint64) {
	if value != nil {
		*target = *value
	}
}

func uniqueObject(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalidRequest
	}
	names := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return ErrInvalidRequest
		}
		name, ok := token.(string)
		if !ok || names[name] {
			return ErrInvalidRequest
		}
		switch name {
		case "schemaVersion", "pod", "container", "kind", "rawPaths", "durationSeconds", "maxEvents", "maxOutputBytes", "maxMapBytes", "maxPathBytes":
		default:
			return ErrInvalidRequest
		}
		names[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return ErrInvalidRequest
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return ErrInvalidRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidRequest
	}
	return nil
}

func validNamespace(namespace string) bool { return len(validation.IsDNS1123Label(namespace)) == 0 }
