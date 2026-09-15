//go:build linux || darwin

package workerinstall

import (
	"io"
	"os"
	"syscall"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func (p *Policy) Programme(root *os.Root, id filecache.ArtifactID) (*filecache.Programme, error) {
	if p == nil || root == nil {
		return nil, ErrInstallation
	}
	file, err := root.OpenFile("candidate-index.json", os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrInstallation
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4096 {
		return nil, ErrInstallation
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || p.verifier.CheckIndex(data, p.indexSHA256) != nil {
		return nil, ErrInstallation
	}
	programme, err := p.verifier.Load(root, id)
	if err != nil {
		return nil, ErrInstallation
	}
	return programme, nil
}
