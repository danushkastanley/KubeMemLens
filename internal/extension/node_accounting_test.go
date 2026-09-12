package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestAccountingConfigurationIsBoundedAndRequiresEvidence(t *testing.T) {
	now := time.Now().UTC()
	valid := nodeanalysis.Qualification{NodeUID: "uid-a", NodeStartedAt: now.Add(-time.Hour), ValidFrom: now, ExpiresAt: now.Add(time.Hour),
		EvidenceSHA256: strings.Repeat("a", 64), UsageDefinition: nodeanalysis.ChargeInclusive, DisjointSystems: []nodecontext.SystemCategory{nodecontext.Kubelet}}
	path := filepath.Join(t.TempDir(), "accounting.json")
	for name, values := range map[string][]nodeanalysis.Qualification{"valid": {valid}, "duplicate": {valid, valid}} {
		data, _ := json.Marshal(values)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadNodeAccounting(path)
		if (err == nil) != (name == "valid") {
			t.Fatalf("%s: %v", name, err)
		}
		if err == nil && len(result) != 1 {
			t.Fatal("qualification lost")
		}
	}
	for _, body := range []string{`[{}]`, `[] {}`, `[{"nodeUID":"` + strings.Repeat("x", 3000) + `"}]`, strings.Repeat(" ", maxAccountingFileBytes+1)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadNodeAccounting(path); err == nil {
			t.Fatal("invalid accounting configuration accepted")
		}
	}
}
