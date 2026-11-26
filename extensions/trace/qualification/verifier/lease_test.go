package verifier

import (
	"errors"
	"strings"
	"testing"
)

type memoryProbeStore struct {
	lines             []string
	commands          []string
	failAt            int
	applyFailed       bool
	readError         bool
	reads, failReadAt int
}

func (s *memoryProbeStore) registry() ([]byte, error) {
	s.reads++
	if s.readError || s.reads == s.failReadAt {
		return nil, errors.New("read unavailable")
	}
	return []byte(strings.Join(s.lines, "\n") + "\n"), nil
}

func (s *memoryProbeStore) command(command string) error {
	s.commands = append(s.commands, command)
	failed := s.failAt == len(s.commands)
	if !failed || s.applyFailed {
		if strings.HasPrefix(command, "-:") {
			kept := s.lines[:0]
			for _, line := range s.lines {
				_, name, _ := strings.Cut(strings.Fields(line)[0], ":")
				if name != strings.TrimPrefix(command, "-:") {
					kept = append(kept, line)
				}
			}
			s.lines = kept
		} else {
			s.lines = append(s.lines, command)
		}
	}
	if failed {
		return errors.New("write outcome uncertain")
	}
	return nil
}

func TestLeasePreservesForeignDefinitionsAndRemovesOnlyItsOwn(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	s := &memoryProbeStore{lines: []string{"p:foreign/event arbitrary_symbol"}}
	lease, err := installProbes(plan, s)
	if err != nil || lease.verify() != nil {
		t.Fatal("owned installation failed")
	}
	if err := lease.close(); err != nil {
		t.Fatal(err)
	}
	if len(s.lines) != 1 || s.lines[0] != "p:foreign/event arbitrary_symbol" || len(s.commands) != 6 {
		t.Fatal("cleanup touched foreign definitions or missed owned probes")
	}
	if lease.close() != nil || len(s.commands) != 6 {
		t.Fatal("repeated close repeated mutations")
	}
}

func TestLeaseRefusesCollisionsAndChangedRegistrations(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	definitions, _ := plan.Definitions()
	s := &memoryProbeStore{lines: []string{definitions[0].Registration}}
	if lease, err := installProbes(plan, s); err == nil || lease != nil || len(s.commands) != 0 {
		t.Fatal("existing event adopted")
	}
	s = &memoryProbeStore{}
	lease, err := installProbes(plan, s)
	if err != nil {
		t.Fatal(err)
	}
	s.lines[0] += "+4"
	if lease.verify() == nil || lease.close() == nil || len(s.commands) != 3 {
		t.Fatal("changed definitions authorised cleanup")
	}
}

func TestUncertainInstallAndRemovalWritesRemainExplicit(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	for _, applied := range []bool{false, true} {
		s := &memoryProbeStore{failAt: 2, applyFailed: applied}
		lease, err := installProbes(plan, s)
		if err == nil || lease == nil {
			t.Fatal("partial install became success or lost cleanup ownership")
		}
		s.failAt = 0
		if lease.close() != nil || len(s.lines) != 0 {
			t.Fatal("partial owned installation not cleaned")
		}
	}
	s := &memoryProbeStore{failAt: 4, applyFailed: true}
	lease, err := installProbes(plan, s)
	if err != nil {
		t.Fatal(err)
	}
	if lease.close() == nil || len(s.lines) != 0 {
		t.Fatal("uncertain deletion falsely succeeded or leaked confirmed probes")
	}
	if lease.close() == nil {
		t.Fatal("repeated cleanup erased uncertain deletion")
	}
	s = &memoryProbeStore{readError: true}
	if _, err := installProbes(plan, s); err == nil || len(s.commands) != 0 {
		t.Fatal("unknown registry authorised mutation")
	}
}

func TestLeaseReconcilesAReadFailureAfterRegistration(t *testing.T) {
	plan, _ := NewProbePlan(strings.Repeat("a", 32))
	s := &memoryProbeStore{failReadAt: 3}
	lease, err := installProbes(plan, s)
	if err == nil || lease == nil || len(s.lines) != 1 {
		t.Fatal("uncertain registered event lost")
	}
	if lease.close() != nil || len(s.lines) != 0 {
		t.Fatal("confirmed attempted event was not cleaned")
	}
}
