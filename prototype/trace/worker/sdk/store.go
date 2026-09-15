// Package sdk constrains the approved Inspektor Gadget image operator.
package sdk

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const objectMediaType = "application/vnd.gadget.ebpf.program.v1+binary"

var ErrWorker = errors.New("constrained trace worker failed")

// The image operator can fetch only the already verified ELF. There is no
// registry client, writable OCI store, fallback reference or arbitrary layer.
type objectStore struct {
	descriptor ocispec.Descriptor
	object     []byte
}

func newStore(p *filecache.Programme) (*objectStore, error) {
	if p == nil {
		return nil, ErrWorker
	}
	object := p.Object()
	m := p.Manifest()
	if len(object) == 0 || len(object) > filecache.MaxObjectBytes || digest.FromBytes(object).String() != "sha256:"+m.ObjectSHA256 {
		return nil, ErrWorker
	}
	return &objectStore{ocispec.Descriptor{MediaType: objectMediaType, Digest: digest.FromBytes(object), Size: int64(len(object))}, object}, nil
}

func (s *objectStore) matches(d ocispec.Descriptor) bool {
	return d.MediaType == s.descriptor.MediaType && d.Digest == s.descriptor.Digest && d.Size == s.descriptor.Size && len(d.URLs) == 0 && len(d.Data) == 0
}
func (s *objectStore) Fetch(ctx context.Context, d ocispec.Descriptor) (io.ReadCloser, error) {
	if ctx.Err() != nil || !s.matches(d) {
		return nil, ErrWorker
	}
	return io.NopCloser(bytes.NewReader(s.object)), nil
}
func (s *objectStore) Exists(ctx context.Context, d ocispec.Descriptor) (bool, error) {
	if ctx.Err() != nil {
		return false, ErrWorker
	}
	return s.matches(d), nil
}
func (s *objectStore) Resolve(ctx context.Context, reference string) (ocispec.Descriptor, error) {
	if ctx.Err() != nil || reference != s.descriptor.Digest.String() {
		return ocispec.Descriptor{}, ErrWorker
	}
	return s.descriptor, nil
}
