package workerinstall

import (
	"github.com/danushkastanley/kube-memlens/prototype/trace/internal/testworker"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if testworker.Cleanup() != nil {
		code = 1
	}
	os.Exit(code)
}
