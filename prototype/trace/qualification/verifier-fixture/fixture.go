// Package verifierfixture contains three fixed, non-attaching calibration loads.
// It is separate from the accepted trace worker and is never a production path.
package verifierfixture

import "errors"

type Case string

const (
	NoLog    Case = "accepted-no-log"
	WithLog  Case = "accepted-with-log"
	Rejected Case = "rejected-with-log"
)

var ErrFixture = errors.New("verifier calibration incomplete")

type definition struct {
	code  [16]byte
	level uint32
}

func describe(which Case) (definition, error) {
	// MOV64 r0, 0; EXIT. The rejected variant writes r1 instead, so r0 is
	// uninitialised at exit. Neither programme contains a helper call or jump.
	d := definition{code: [16]byte{0xb7, 0, 0, 0, 0, 0, 0, 0, 0x95, 0, 0, 0, 0, 0, 0, 0}}
	switch which {
	case NoLog:
	case WithLog:
		d.level = 1
	case Rejected:
		d.level = 1
		d.code[1] = 1
	default:
		return definition{}, ErrFixture
	}
	return d, nil
}

// Outcome is private calibration evidence. Programme IDs identify only this
// fixture's newly loaded objects so the controller can verify their removal.
type Outcome struct {
	Case              Case   `json:"case"`
	Accepted          bool   `json:"accepted"`
	Errno             uint32 `json:"errno"`
	ProgrammeID       uint32 `json:"programmeID"`
	LogBufferBytes    uint32 `json:"logBufferBytes"`
	RequiredLogBytes  uint32 `json:"requiredLogBytes"`
	ObservedLogBytes  uint32 `json:"observedLogBytes"`
	SyscallUpperNanos uint64 `json:"syscallUpperNanos"`
}
