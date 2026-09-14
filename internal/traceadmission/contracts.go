package traceadmission

import (
	"context"
	"errors"
	"fmt"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"io"
	"k8s.io/apiserver/pkg/authentication/user"
	"time"
)

const APIGroup = "tracing.kubememlens.io"
const APIVersion = "v1alpha1"

type Operation string

const (
	Create Operation = "create"
	Read   Operation = "get"
	Cancel Operation = "delete"
	Attach Operation = "stream"
)

// Authorizer checks trace-resource permission and exact Pod read permission.
// Implementations must evaluate current policy and honour context cancellation.
type Authorizer interface {
	Trace(context.Context, user.Info, Operation, string, string) error
	Pod(context.Context, user.Info, string, string) error
}

// Workload is an authoritative pre-binding lifetime. Target.CgroupID must be
// zero here: only the selected node can supply its kernel identity.
type Workload struct {
	Target   trace.TargetIdentity
	NodeName string
	QoS      string
}

func (Workload) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[trace workload]") }
func (Workload) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace workload requires private transport")
}

type Resolver interface {
	Resolve(context.Context, Request) (Workload, error)
	Revalidate(context.Context, Workload) error
}

// Binding owns an expiring node-side handle. Target and profile are immutable.
// Methods must honour deadlines and support concurrent revalidation and Close;
// Close is idempotent. Node expiry is independent of controller connectivity.
type Binding interface {
	Target() trace.TargetIdentity
	ProfileDigest() string
	Revalidate(context.Context) error
	Close(context.Context) error
}

// Binder returns a cleanup-only Binding with an error when node work might have
// begun but its response was lost. Admission retains that handle and quota until
// closure is confirmed; it must never treat a transport error as proof of absence.
type Binder interface {
	Bind(context.Context, string, Workload, Request, time.Time) (Binding, error)
}

type AuditEvent struct {
	Operation Operation
	Decision  string
	Reason    string
	Principal string
	Kind      trace.Kind
}

// Audit receives only fixed categories, never the request or authenticated name.
type Audit func(AuditEvent)

type AdmissionState string

const (
	AdmittedState AdmissionState = "admitted"
	ActiveState   AdmissionState = "active"
)

type Admission struct {
	state         AdmissionState
	id            string
	specification trace.Specification
	engineDigest  string
	expiresAt     time.Time
}

func (a Admission) State() AdmissionState              { return a.state }
func (a Admission) ID() string                         { return a.id }
func (a Admission) Specification() trace.Specification { return a.specification }
func (a Admission) EngineDigest() string               { return a.engineDigest }
func (a Admission) ExpiresAt() time.Time               { return a.expiresAt }
func (Admission) Format(w fmt.State, _ rune)           { _, _ = io.WriteString(w, "[trace admission]") }
func (Admission) MarshalJSON() ([]byte, error) {
	return nil, errors.New("trace admission requires an authorised representation")
}
