package targetfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestLeafStatRequiresOneExplicitZeroVisibleDescendantCount(t *testing.T) {
	for _, data := range []string{"nr_descendants 0\n", "nr_dying_descendants 12\nnr_descendants 0\nnr_subsys_memory 1\nnr_dying_subsys_memory 4\n"} {
		if leafStat([]byte(data)) != nil {
			t.Fatal("visible leaf rejected")
		}
	}
	for name, data := range map[string]string{
		"empty": "", "missing": "nr_dying_descendants 0\n", "nonleaf": "nr_descendants 1\n",
		"duplicate": "nr_descendants 0\nnr_descendants 0\n", "conflicting": "nr_descendants 0\nnr_descendants 1\n",
		"negative": "nr_descendants -1\n", "signed-zero": "nr_descendants +0\n", "leading-zero": "nr_descendants 00\n",
		"overflow": "nr_descendants 18446744073709551616\n", "missing-value": "nr_descendants\n",
		"empty-value": "nr_descendants \n", "extra-value": "nr_descendants 0 1\n", "tab": "nr_descendants\t0\n",
		"nul": "nr_descendants 0\x00\n", "malformed-extra": "nr_descendants 0\ninvalid\n",
		"oversize": "nr_descendants 0\n" + strings.Repeat("x", maxLeafStatBytes),
	} {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(leafStat([]byte(data)), admission.ErrUnavailable) {
				t.Fatal("missing, ambiguous or invalid leaf evidence accepted")
			}
		})
	}
}

func TestLeafReadIsBoundedAndDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	file, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	path := filepath.Join(root, "cgroup.stat")
	if err := leaf(int(file.Fd())); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal("missing counter accepted")
	}
	outside := filepath.Join(t.TempDir(), "counter")
	if err := os.WriteFile(outside, []byte("nr_descendants 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := leaf(int(file.Fd())); !errors.Is(err, admission.ErrTargetChanged) {
		t.Fatal("counter symlink followed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("nr_descendants 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := leaf(int(file.Fd())); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("nr_descendants 0\n"+strings.Repeat("x", maxLeafStatBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := leaf(int(file.Fd())); !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("oversized counter accepted")
	}
	file.Close()
	if leaf(int(file.Fd())) == nil {
		t.Fatal("closed descriptor accepted")
	}
}
