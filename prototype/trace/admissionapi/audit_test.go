package admissionapi

import (
	"bytes"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
)

func testAuditReferences(t *testing.T) *traceaudit.References {
	t.Helper()
	refs, err := traceaudit.NewReferences(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return refs
}
