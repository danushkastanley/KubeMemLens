package qualification

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPreflightPolicyCannotAttachPinOrEnumerateGlobalObjects(t *testing.T) {
	data, err := os.ReadFile("../seccomp/preflight.json")
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
				Value int
				Op    string
			}
		}
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.DefaultAction != "SCMP_ACT_ERRNO" {
		t.Fatal("policy does not deny by default")
	}
	required := map[int]bool{0: false, 5: false, 15: false}
	forbidden := map[string]bool{"mount": true, "setns": true, "unshare": true, "perf_event_open": true, "ptrace": true, "socket": true, "connect": true, "sendmsg": true}
	for _, rule := range policy.Syscalls {
		if rule.Action != "SCMP_ACT_ALLOW" {
			continue
		}
		for _, name := range rule.Names {
			if forbidden[name] {
				t.Fatalf("forbidden syscall %s", name)
			}
			if name != "bpf" {
				continue
			}
			if len(rule.Names) != 1 || len(rule.Args) != 1 {
				t.Fatal("unbounded BPF rule")
			}
			arg := rule.Args[0]
			_, ok := required[arg.Value]
			if !ok || arg.Index != 0 || arg.Op != "SCMP_CMP_EQ" {
				t.Fatal("unapproved BPF command")
			}
			required[arg.Value] = true
		}
	}
	for command, found := range required {
		if !found {
			t.Fatalf("missing baseline command %d", command)
		}
	}
}
