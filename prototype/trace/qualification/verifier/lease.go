package verifier

import (
	"errors"
	"fmt"
	"maps"
)

// probeStore is the narrow tracefs boundary. Implementations must bound reads
// and append individual commands without truncating the global registry.
type probeStore interface {
	registry() ([]byte, error)
	command(string) error
}

type probeLease struct {
	plan     ProbePlan
	store    probeStore
	owned    map[string]string
	closed   bool
	closeErr error
}

// installProbes refuses an existing group. It returns a lease on a partial
// failure so the caller can attempt only the cleanup supported by ownership
// evidence. Attempted definitions are remembered before writing; a fresh exact
// registry match is still required before any later deletion.
func installProbes(plan ProbePlan, store probeStore) (*probeLease, error) {
	definitions, err := plan.Definitions()
	if err != nil || store == nil {
		return nil, ErrObservation
	}
	p := &probeLease{plan: plan, store: store, owned: make(map[string]string)}
	current, err := p.read()
	if err != nil || len(current) != 0 {
		return nil, ErrObservation
	}
	for index, definition := range definitions {
		current, err = p.read()
		if err != nil || !maps.Equal(current, p.owned) {
			return p, ErrObservation
		}
		p.owned[definition.Name] = definition.Registration
		writeErr := store.command(definition.Registration)
		current, err = p.read()
		if err != nil {
			return p, ErrObservation
		}
		if current[definition.Name] == "" {
			delete(p.owned, definition.Name)
		}
		if writeErr != nil || !maps.Equal(current, p.owned) || p.owned[definition.Name] == "" {
			return p, fmt.Errorf("verifier probe registration %d: %w", index, errors.Join(ErrObservation, writeErr))
		}
	}
	return p, nil
}

func (p *probeLease) read() (map[string]string, error) {
	raw, err := p.store.registry()
	if err != nil {
		return nil, ErrObservation
	}
	return p.plan.ValidateRegistry(raw)
}

func (p *probeLease) verify() error {
	current, err := p.read()
	if p.closed || err != nil || !maps.Equal(current, p.owned) || len(current) != 3 {
		return ErrObservation
	}
	return nil
}

// close removes only successfully reconciled definitions, in reverse order.
// A changed registry fails before a deletion. Perf descriptors must be closed
// first; an EBUSY or uncertain deletion is reported and never called complete.
func (p *probeLease) close() (result error) {
	if p.closed {
		return p.closeErr
	}
	defer func() { p.closeErr = errors.Join(p.closeErr, result) }()
	definitions, err := p.plan.Definitions()
	if err != nil {
		return err
	}
	for index := len(definitions) - 1; index >= 0; index-- {
		definition := definitions[index]
		if p.owned[definition.Name] == "" {
			continue
		}
		current, err := p.read()
		if err != nil {
			return ErrObservation
		}
		for name, registration := range current {
			if p.owned[name] != registration {
				return ErrObservation
			}
		}
		for name := range p.owned {
			if current[name] == "" {
				delete(p.owned, name)
				result = errors.Join(result, ErrObservation)
			}
		}
		if p.owned[definition.Name] == "" {
			continue
		}
		writeErr := p.store.command(definition.Removal)
		current, err = p.read()
		if err != nil {
			return ErrObservation
		}
		if _, exists := current[definition.Name]; !exists {
			delete(p.owned, definition.Name)
		}
		if writeErr != nil || !maps.Equal(current, p.owned) || p.owned[definition.Name] != "" {
			result = errors.Join(result, ErrObservation)
		}
	}
	if len(p.owned) == 0 {
		current, err := p.read()
		if err != nil || len(current) != 0 {
			return ErrObservation
		}
		p.closed = true
	}
	return errors.Join(p.closeErr, result)
}
