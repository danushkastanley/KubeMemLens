package kube

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// SessionPodIdentity is the minimum live binding for a scoped memory capture.
// Callers must authorise the requesting user before invoking this privileged read.
type SessionPodIdentity struct{ UID, NodeName string }

func (r *NamespaceIdentityReader) LookupPod(ctx context.Context, namespace, name string) (SessionPodIdentity, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 || len(validation.IsDNS1123Subdomain(name)) != 0 {
		return SessionPodIdentity{}, incidentsession.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := healthQuery{reader: r.reader, remaining: maxHealthResponse, validateJSON: boundedVolumeObject}
	var pod corev1.Pod
	if q.get(ctx, "/api/v1/namespaces/"+namespace+"/pods/"+name, &pod) != nil {
		return SessionPodIdentity{}, incidentsession.ErrUnavailable
	}
	if pod.APIVersion != "v1" || pod.Kind != "Pod" || pod.Namespace != namespace || pod.Name != name || pod.UID == "" || len(pod.UID) > 128 || pod.Spec.NodeName == "" || len(validation.IsDNS1123Subdomain(pod.Spec.NodeName)) != 0 {
		return SessionPodIdentity{}, incidentsession.ErrUnavailable
	}
	return SessionPodIdentity{UID: string(pod.UID), NodeName: pod.Spec.NodeName}, nil
}
