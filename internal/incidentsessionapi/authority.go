// Package incidentsessionapi binds the internal session domain to authenticated
// Kubernetes requests. It never accepts caller-supplied actors or namespace UIDs.
package incidentsessionapi

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kubeprincipal"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

const Resource = "incidentsessions"
const SchemaHeader = incidentsession.SchemaHeader

type NamespaceResolver interface {
	Lookup(context.Context, string) (string, error)
}

type Authority struct {
	namespaces map[string]struct{}
	resolver   NamespaceResolver
	delegate   authorizer.Authorizer
}

func ValidateNamespaces(names []string) error {
	if len(names) == 0 || len(names) > 64 {
		return incidentsession.ErrInvalid
	}
	seen := map[string]bool{}
	for _, name := range names {
		if len(validation.IsDNS1123Label(name)) != 0 || seen[name] {
			return incidentsession.ErrInvalid
		}
		seen[name] = true
	}
	return nil
}

func NewAuthority(names []string, resolver NamespaceResolver, delegate authorizer.Authorizer) (*Authority, error) {
	if ValidateNamespaces(names) != nil || resolver == nil || delegate == nil {
		return nil, incidentsession.ErrInvalid
	}
	a := &Authority{namespaces: map[string]struct{}{}, resolver: resolver, delegate: delegate}
	for _, name := range names {
		a.namespaces[strings.Clone(name)] = struct{}{}
	}
	return a, nil
}

func (a *Authority) Resolve(ctx context.Context, namespace string, operation incidentsession.Operation, id string) (incidentsession.Principal, error) {
	info, _ := apirequest.UserFrom(ctx)
	principal, key, err := kubeprincipal.SnapshotPrincipal(info)
	if err != nil {
		return incidentsession.Principal{}, kubeprincipal.ErrUnauthenticated
	}
	if _, allowed := a.namespaces[namespace]; !allowed {
		return incidentsession.Principal{}, incidentsession.ErrNotFound
	}
	verb, subresource, err := attributes(operation)
	if err != nil || (operation == incidentsession.Create && id != "") || (operation != incidentsession.Create && !validID(id)) {
		return incidentsession.Principal{}, incidentsession.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	decision, _, err := a.delegate.Authorize(ctx, authorizer.AttributesRecord{User: principal, Verb: verb, Namespace: namespace,
		APIGroup: api.MemoryAPIGroup, APIVersion: api.MemoryAPIVersion, Resource: Resource, Subresource: subresource, Name: id, ResourceRequest: true})
	if err != nil {
		return incidentsession.Principal{}, incidentsession.ErrUnavailable
	}
	if decision != authorizer.DecisionAllow {
		return incidentsession.Principal{}, incidentsession.ErrDenied
	}
	uid, err := a.resolver.Lookup(ctx, namespace)
	if err != nil {
		return incidentsession.Principal{}, err
	}
	if uid == "" || len(uid) > 128 || ctx.Err() != nil {
		return incidentsession.Principal{}, incidentsession.ErrUnavailable
	}
	return incidentsession.Principal{Namespace: namespace, NamespaceUID: uid, Actor: hex.EncodeToString(key[:])}, nil
}

// Re-evaluate delegated policy and namespace lifetime for each store operation,
// including after evidence acquisition. Group changes do not create a new owner.
func (a *Authority) Authorize(ctx context.Context, p incidentsession.Principal, op incidentsession.Operation, id string) error {
	current, err := a.Resolve(ctx, p.Namespace, op, id)
	if err != nil {
		return err
	}
	if current != p {
		return incidentsession.ErrDenied
	}
	return nil
}

func validID(id string) bool {
	if len(id) != 32 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func attributes(op incidentsession.Operation) (string, string, error) {
	switch op {
	case incidentsession.Create:
		return "create", "", nil
	case incidentsession.Markers:
		return "create", "markers", nil
	case incidentsession.Compare:
		return "create", "compare", nil
	case incidentsession.Capture:
		return "create", "capture", nil
	case incidentsession.ReferenceTrace:
		return "create", incidentsession.TraceReferenceSubresource, nil
	case incidentsession.Inspect:
		return "get", "", nil
	case incidentsession.Append:
		return "create", "entries", nil
	case incidentsession.CloseSession:
		return "create", "close", nil
	case incidentsession.Delete:
		return "delete", "", nil
	case incidentsession.ExportSanitised:
		return "get", "export", nil
	case incidentsession.ExportAuthorised:
		return "get", "export-sensitive", nil
	default:
		return "", "", incidentsession.ErrInvalid
	}
}
