// Package kubeprincipal snapshots delegated identities without server or client dependencies.
package kubeprincipal

import (
	"crypto/sha256"
	"errors"
	"strings"
	"unicode/utf8"

	"k8s.io/apiserver/pkg/authentication/user"
)

var ErrUnauthenticated = errors.New("authenticated Kubernetes identity is required")

const maxPrincipalBytes = 16 << 10

// SnapshotPrincipal is called only with the authenticated server context. It
// copies every delegated identity field so request buffers cannot mutate a
// reservation. Group and extra claims remain available for fresh SAR calls.
func SnapshotPrincipal(info user.Info) (*user.DefaultInfo, [32]byte, error) {
	var key [32]byte
	if info == nil {
		return nil, key, ErrUnauthenticated
	}
	name, uid := info.GetName(), info.GetUID()
	if name == "" || name == user.Anonymous || len(name) > 1024 || len(uid) > 1024 {
		return nil, key, ErrUnauthenticated
	}
	groups, extras := info.GetGroups(), info.GetExtra()
	if len(groups) > 128 || len(extras) > 32 {
		return nil, key, ErrUnauthenticated
	}
	total := len(name) + len(uid)
	if !identityText(name) || !identityText(uid) {
		return nil, key, ErrUnauthenticated
	}
	authenticated := false
	for _, group := range groups {
		if group == user.AllUnauthenticated || len(group) > 1024 || !identityText(group) {
			return nil, key, ErrUnauthenticated
		}
		authenticated = authenticated || group == user.AllAuthenticated
		total += len(group)
	}
	if !authenticated {
		return nil, key, ErrUnauthenticated
	}
	copied := &user.DefaultInfo{Name: name, UID: uid, Groups: append([]string(nil), groups...), Extra: map[string][]string{}}
	for name, values := range extras {
		if len(name) > 1024 || !identityText(name) || len(values) > 32 {
			return nil, key, ErrUnauthenticated
		}
		total += len(name)
		for _, value := range values {
			if len(value) > 1024 || !identityText(value) {
				return nil, key, ErrUnauthenticated
			}
			total += len(value)
		}
		if total > maxPrincipalBytes {
			return nil, key, ErrUnauthenticated
		}
		copied.Extra[name] = append([]string(nil), values...)
	}
	if total > maxPrincipalBytes {
		return nil, key, ErrUnauthenticated
	}
	// Kubernetes names and optional UIDs identify the requester. Mutable group
	// membership must not provide a second quota bucket for the same requester.
	key = sha256.Sum256([]byte(name + "\x00" + uid))
	return copied, key, nil
}

func identityText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f })
}
