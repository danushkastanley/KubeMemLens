// Package tracecompat negotiates public trace contracts before activation.
// A contract names wire semantics; it never selects incident programmes or limits.
package tracecompat

import (
	"errors"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const Header = "X-KubeMemLens-Trace-Contract"
const Minimum = 1
const Maximum = 1
const Offer = "1-1"
const Selected = "1"
const StreamQuery = "contract=1"
const RequestSchema = 3

type Version uint16

const Legacy Version = 0
const Current Version = 1

var ErrInvalid = errors.New("invalid trace contract offer")
var ErrIncompatible = errors.New("no compatible trace contract")

// Range is an inclusive, nonzero contract range, not an executable version range.
type Range struct{ Min, Max uint16 }

func (r Range) valid() bool { return r.Min > 0 && r.Max >= r.Min }

// Negotiate accepts one canonical min-max header. Absence retains the previous
// development API only while contract 1 is supported. Repeated/combined headers,
// leading zeroes, signs and whitespace cannot create ambiguous offers.
func Negotiate(values []string, supported Range) (Version, error) {
	if !supported.valid() {
		return Legacy, ErrInvalid
	}
	if len(values) == 0 {
		if supported.Min != Minimum {
			return Legacy, ErrIncompatible
		}
		return Legacy, nil
	}
	if len(values) != 1 || len(values[0]) > 11 {
		return Legacy, ErrInvalid
	}
	parts := strings.Split(values[0], "-")
	if len(parts) != 2 {
		return Legacy, ErrInvalid
	}
	low, err := number(parts[0])
	if err != nil {
		return Legacy, err
	}
	high, err := number(parts[1])
	if err != nil || high < low {
		return Legacy, ErrInvalid
	}
	selected := min(high, supported.Max)
	if selected < max(low, supported.Min) {
		return Legacy, ErrIncompatible
	}
	return Version(selected), nil
}

func number(text string) (uint16, error) {
	value, err := strconv.ParseUint(text, 10, 16)
	if err != nil || value == 0 || strconv.FormatUint(value, 10) != text {
		return 0, ErrInvalid
	}
	return uint16(value), nil
}

// AcceptResponse requires the exact negotiated contract, never a missing-header
// downgrade to an older extension that ignored the offer.
func AcceptResponse(values []string) error {
	if len(values) != 1 || values[0] != Selected {
		return ErrIncompatible
	}
	return nil
}

// StreamCompatible compares the installation's fixed format to contract 1.
// Experimental version-1 streams are deliberately outside this contract.
func StreamCompatible(kind trace.Kind, version int) bool {
	switch kind {
	case trace.Files, trace.Cache:
		return version == 2
	case trace.OOM:
		return version == 3
	default:
		return false
	}
}
