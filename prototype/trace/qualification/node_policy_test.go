package qualification

import (
	"encoding/json"
	"os"
	"testing"
)

func TestFileCachePolicyAllowsGoThreadsWithoutNamespaceCreation(t *testing.T) {
	data, err := os.ReadFile("../seccomp/filecache-node.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		DefaultAction string
		Syscalls      []struct {
			Names  []string
			Action string
			Args   []struct {
				Index int
				Value uint64
				Op    string
			}
		}
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.DefaultAction != "SCMP_ACT_ERRNO" {
		t.Fatal("policy must deny unrecognised clone flags")
	}
	// Go's amd64 runtime supplies CLONE_SETTLS with the shared-thread flags.
	// The two existing process forms remain necessary for worker startup;
	// its synchronised containment filter denies further process descendants.
	const sharedThread = 0x50f00
	const setTLS = 0x80000
	wanted := map[uint64]bool{sharedThread: false, sharedThread | setTLS: false, 16657: false, 20753: false}
	for _, rule := range policy.Syscalls {
		if rule.Action != "SCMP_ACT_ALLOW" {
			continue
		}
		for _, name := range rule.Names {
			if name == "clone3" {
				t.Fatal("clone3 flags cannot be inspected by this policy")
			}
			if name != "clone" {
				continue
			}
			if len(rule.Names) != 1 || len(rule.Args) != 1 {
				t.Fatal("clone requires one exact flags constraint")
			}
			arg := rule.Args[0]
			if _, ok := wanted[arg.Value]; !ok || arg.Index != 0 || arg.Op != "SCMP_CMP_EQ" {
				t.Fatalf("unexpected clone rule: %+v", arg)
			}
			wanted[arg.Value] = true
		}
	}
	for flags, found := range wanted {
		if !found {
			t.Errorf("missing runtime clone flags %#x", flags)
		}
	}
}
