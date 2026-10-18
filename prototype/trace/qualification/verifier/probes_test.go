package verifier

import (
	"strings"
	"testing"
)

func TestProbePlanOnlyEmitsFixedSymbolicNumericProbes(t *testing.T) {
	plan, err := NewProbePlan(strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := plan.Definitions()
	if err != nil || len(definitions) != 3 {
		t.Fatal("fixed set changed")
	}
	for _, d := range definitions {
		if d.Removal != "-:"+d.Name || strings.ContainsAny(d.Registration, "\n\r") {
			t.Fatal("definition escaped the owned event")
		}
	}
	if !strings.HasSuffix(definitions[2].Registration, "bpf_vlog_finalize result=$retval:s32 bytes=+0($arg2):u32") {
		t.Fatal("log observation broadened beyond numeric finalizer output")
	}
	for _, owner := range []string{"", "other", strings.Repeat("A", 32), strings.Repeat("a", 31), strings.Repeat("a", 32) + "\n", "../../foreign"} {
		if _, err := NewProbePlan(owner); err == nil {
			t.Fatal("unbounded owner accepted")
		}
	}
	if _, err := (ProbePlan{}).Definitions(); err == nil {
		t.Fatal("zero plan emitted commands")
	}
}

func TestRegistryDoesNotAdoptForeignChangedOrDuplicateDefinitions(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	definitions, _ := plan.Definitions()
	lines := []string{"p:foreign/enter other_symbol"}
	for _, d := range definitions {
		lines = append(lines, d.Registration)
	}
	raw := strings.Join(lines, "\n") + "\n"
	found, err := plan.ValidateRegistry([]byte(raw))
	if err != nil || len(found) != 3 {
		t.Fatal("owned definition registry rejected")
	}
	for _, changed := range []string{
		raw + lines[1] + "\n",
		strings.Replace(raw, "bytes=+0($arg2):u32", "bytes=+0($arg2):u64", 1),
		strings.Replace(raw, "bpf_check", "bpf_check+4", 1),
		strings.Replace(raw, "r128:", "r256:", 1),
		strings.Replace(raw, "r128:", "r:", 1),
		raw + "p:" + plan.group + "/unexpected arbitrary_function\n",
		raw + "\x00",
		strings.Repeat("x", (1<<20)+1),
	} {
		if _, err := plan.ValidateRegistry([]byte(changed)); err == nil {
			t.Fatal("changed owned registry accepted")
		}
	}
	found, err = plan.ValidateRegistry([]byte(lines[0] + "\n"))
	if err != nil || len(found) != 0 {
		t.Fatal("foreign definition was adopted")
	}
}

func TestProfileRequiresEveryUniqueOwnedCounterWithoutForeignData(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	definitions, _ := plan.Definitions()
	lines := []string{"unrelated 999 999"}
	for _, d := range definitions {
		_, name, _ := strings.Cut(d.Name, "/")
		lines = append(lines, name+" 10 2")
	}
	raw := strings.Join(lines, "\n") + "\n"
	counts, err := plan.Profile([]byte(raw))
	if err != nil || len(counts) != 3 {
		t.Fatal("owned counters unavailable")
	}
	for name, count := range counts {
		if !strings.HasSuffix(name, strings.Repeat("a", 32)) || count.Hits != 10 || count.Missed != 2 {
			t.Fatal("wrong owner or counter projection")
		}
	}
	for _, changed := range []string{
		strings.Join(lines[:3], "\n"), raw + lines[1] + "\n",
		strings.Replace(raw, " 10 2", " 10 -1", 1),
		strings.Replace(raw, " 10 2", " 18446744073709551616 2", 1),
		strings.Replace(raw, " 10 2", " 10", 1), raw + "\x00",
	} {
		if _, err := plan.Profile([]byte(changed)); err == nil {
			t.Fatal("missing or changed counter became evidence")
		}
	}
}
