package verifier

import (
	"regexp"
	"strconv"
	"strings"
)

var ownerPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ProbePlan only describes the fixed observer. Creating a plan does not attach
// it or grant authority to change tracefs. Callers cannot supply fetch expressions.
type ProbePlan struct {
	group string
}

type ProbeDefinition struct {
	Name         string `json:"name"`
	Registration string `json:"registration"`
	Removal      string `json:"removal"`
}

func NewProbePlan(owner string) (ProbePlan, error) {
	if !ownerPattern.MatchString(owner) {
		return ProbePlan{}, ErrObservation
	}
	return ProbePlan{group: "kml_verifier_" + owner}, nil
}

func (p ProbePlan) Definitions() ([]ProbeDefinition, error) {
	if !strings.HasPrefix(p.group, "kml_verifier_") || !ownerPattern.MatchString(strings.TrimPrefix(p.group, "kml_verifier_")) {
		return nil, ErrObservation
	}
	result := make([]ProbeDefinition, 0, 3)
	for _, item := range []struct{ name, prefix, target string }{
		{"enter", "p:", "bpf_check"},
		{"return", "r128:", "bpf_check result=$retval:s32"},
		{"log", "r128:", "bpf_vlog_finalize result=$retval:s32 bytes=+0($arg2):u32"},
	} {
		// kprobe_profile omits the group, so the event basename must also be
		// unique to this owner when checking missed-return counters.
		name := p.group + "/" + item.name + "_" + strings.TrimPrefix(p.group, "kml_verifier_")
		result = append(result, ProbeDefinition{Name: name, Registration: item.prefix + name + " " + item.target,
			Removal: "-:" + name})
	}
	return result, nil
}

// ValidateRegistry identifies only this exact group in a bounded kernel
// kprobe_events snapshot. Definitions outside the group are never returned.
// No other target, maxactive, offset, fetch expression or additional event in
// the group is accepted. Unsupported kernel serialization fails closed.
func (p ProbePlan) ValidateRegistry(raw []byte) (map[string]string, error) {
	definitions, err := p.Definitions()
	if err != nil || len(raw) > 1<<20 || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, ErrObservation
	}
	expected := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		expected[definition.Name] = definition.Registration
	}
	found := make(map[string]string, len(definitions))
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		_, name, ok := strings.Cut(fields[0], ":")
		if !ok || !strings.HasPrefix(name, p.group+"/") {
			continue
		}
		normalized := strings.Join(fields, " ")
		want, exists := expected[name]
		if !exists || found[name] != "" || normalized != want {
			return nil, ErrObservation
		}
		found[name] = normalized
	}
	return found, nil
}

type ProbeCount struct {
	Hits   uint64 `json:"hits"`
	Missed uint64 `json:"missed"`
}

// Profile reads the unique basenames used by kprobe_profile. Missing, duplicate
// or malformed owned rows cannot become zero miss counts. No foreign names or
// counters are returned.
func (p ProbePlan) Profile(raw []byte) (map[string]ProbeCount, error) {
	definitions, err := p.Definitions()
	if err != nil || len(raw) > 1<<20 || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, ErrObservation
	}
	names := make(map[string]bool, len(definitions))
	for _, d := range definitions {
		_, name, _ := strings.Cut(d.Name, "/")
		names[name] = true
	}
	found := make(map[string]ProbeCount, len(definitions))
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !names[fields[0]] {
			continue
		}
		if _, exists := found[fields[0]]; exists || len(fields) != 3 {
			return nil, ErrObservation
		}
		hits, hitErr := strconv.ParseUint(fields[1], 10, 64)
		missed, missErr := strconv.ParseUint(fields[2], 10, 64)
		if hitErr != nil || missErr != nil {
			return nil, ErrObservation
		}
		found[fields[0]] = ProbeCount{hits, missed}
	}
	if len(found) != len(definitions) {
		return nil, ErrObservation
	}
	return found, nil
}
