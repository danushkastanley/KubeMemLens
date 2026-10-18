// Package traceaudit builds bounded audit evidence without retaining trace payloads.
package traceaudit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"k8s.io/apimachinery/pkg/util/validation"
)

var ErrInvalid = errors.New("trace audit input is invalid")

// References owns an installation key. It contains no identity mapping, payload
// or result store. Administrators retain the key separately to verify references.
type References struct {
	key   [32]byte
	keyID string
}

func NewReferences(key []byte) (*References, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	r := &References{}
	copy(r.key[:], key)
	digest := sha256.Sum256(key)
	r.keyID = "sha256:" + hex.EncodeToString(digest[:])
	return r, nil
}
func (*References) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private trace audit references]")
}
func (*References) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }
func (r *References) KeyID() string {
	if r == nil {
		return ""
	}
	return r.keyID
}

// Identity names and UIDs match admission's owner identity. Mutable groups and
// extra claims are excluded, so a role change cannot create a new actor reference.
func (r *References) Actor(name, uid string) (string, error) {
	if name == "" || !identityText(name) || !identityText(uid) {
		return "", ErrInvalid
	}
	return r.reference("actor", name, uid)
}
func (r *References) Tenant(namespace string) (string, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 {
		return "", ErrInvalid
	}
	return r.reference("tenant", namespace)
}
func (r *References) Session(id string) (string, error) {
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return "", ErrInvalid
	}
	return r.reference("session", id)
}

// Target is available only after the authoritative node binding is known. A
// request's claimed name or unbound lifetime must never become an observed target.
func (r *References) Target(target trace.TargetIdentity) (string, error) {
	if target.ValidateLifetime() != nil || target.CgroupID == 0 {
		return "", ErrInvalid
	}
	return r.reference("target", target.Namespace, target.PodName, target.PodUID, target.ContainerName, target.ContainerID,
		target.ContainerStartedAt.UTC().Format(time.RFC3339Nano), target.NodeUID, strconv.FormatUint(target.CgroupID, 10))
}
func identityText(value string) bool {
	return len(value) <= 1024 && utf8.ValidString(value) && !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
func (r *References) reference(domain string, parts ...string) (string, error) {
	if r == nil || r.keyID == "" {
		return "", ErrInvalid
	}
	digest := hmac.New(sha256.New, r.key[:])
	_, _ = digest.Write([]byte("kubememlens-trace-audit-v1/" + domain))
	for _, part := range parts {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(part)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(part))
	}
	return "hmac-sha256:" + hex.EncodeToString(digest.Sum(nil)), nil
}
