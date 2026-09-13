package trace

import (
	"errors"
	"fmt"
	"io"
)

// SensitiveText can only be exposed deliberately by the authorised ephemeral
// output adapter. Formatting and default JSON encoding never reveal its bytes.
// The adapter must still escape terminal controls and enforce encoded limits.
type SensitiveText struct{ value string }

func NewSensitiveText(value string, maxBytes uint64) (SensitiveText, error) {
	if maxBytes == 0 || maxBytes > 512 || uint64(len(value)) > maxBytes {
		return SensitiveText{}, errors.New("sensitive trace text exceeds policy")
	}
	return SensitiveText{value: value}, nil
}

func (s SensitiveText) Reveal() string { return s.value }

func (SensitiveText) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[sensitive trace text]") }
func (SensitiveText) MarshalJSON() ([]byte, error) {
	return nil, errors.New("sensitive trace text requires explicit ephemeral encoding")
}
