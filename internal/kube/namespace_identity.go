package kube

import (
	"context"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

// NamespaceIdentityReader resolves live namespace lifetime without retaining
// labels or annotations. Its transport shares existing TLS, redirect, timeout
// and response-size controls; it does not watch or list namespace objects.
type NamespaceIdentityReader struct{ reader *volumeHealthReader }

func NewNamespaceIdentityReader(config *rest.Config) (*NamespaceIdentityReader, error) {
	if config == nil || config.Insecure || !strings.HasPrefix(config.Host, "https://") {
		return nil, incidentsession.ErrInvalid
	}
	reader, err := newVolumeHealthReader(config, VolumeHealthOptions{Namespace: "default", Timeout: 5 * time.Second})
	if err != nil {
		return nil, incidentsession.ErrUnavailable
	}
	return &NamespaceIdentityReader{reader: reader}, nil
}

func (r *NamespaceIdentityReader) Close() { r.reader.client.CloseIdleConnections() }

func (r *NamespaceIdentityReader) Lookup(ctx context.Context, namespace string) (string, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return "", incidentsession.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := healthQuery{reader: r.reader, remaining: maxHealthResponse, validateJSON: boundedVolumeObject}
	var value corev1.Namespace
	if q.get(ctx, "/api/v1/namespaces/"+namespace, &value) != nil {
		return "", incidentsession.ErrUnavailable
	}
	if value.APIVersion != "v1" || value.Kind != "Namespace" || value.Name != namespace || value.UID == "" || len(value.UID) > 128 {
		return "", incidentsession.ErrUnavailable
	}
	if value.DeletionTimestamp != nil || value.Status.Phase != corev1.NamespaceActive {
		return "", incidentsession.ErrNotFound
	}
	return string(value.UID), nil
}
