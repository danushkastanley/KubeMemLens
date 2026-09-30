package traceadmission

import (
	"github.com/danushkastanley/kube-memlens/internal/kubeprincipal"
	"k8s.io/apiserver/pkg/authentication/user"
	"strings"
)

func snapshotPrincipal(info user.Info) (*user.DefaultInfo, [32]byte, error) {
	principal, key, err := kubeprincipal.SnapshotPrincipal(info)
	if err != nil {
		return nil, [32]byte{}, ErrUnauthenticated
	}
	return principal, key, nil
}

func principalCategory(info user.Info) string {
	if info == nil {
		return "unauthenticated"
	}
	if strings.HasPrefix(info.GetName(), "system:serviceaccount:") {
		return "serviceaccount"
	}
	return "user"
}
