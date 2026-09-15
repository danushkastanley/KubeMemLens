package trace

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// ValidateContext checks the victim context accepted from the constrained OOM
// worker. Missing values are permitted; contradictory or hidden values are not.
func (event OOMDecision) ValidateContext() error {
	command := event.Command.Reveal()
	if (event.Scope != OOMScopeCgroup && event.Scope != OOMScopeGlobal && event.Scope != OOMScopeUnknown) ||
		(event.VictimPID != nil && (*event.VictimPID == 0 || *event.VictimPID > 1<<31-1)) || len(command) > 16 || !utf8.ValidString(command) || strings.ContainsRune(command, 0) {
		return errors.New("invalid OOM process context")
	}
	return nil
}
