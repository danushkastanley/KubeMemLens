package testworker

import "bytes"

// Diagnostics bounds compiler/fixture diagnostics. Never use it to capture
// production worker stderr or private trace protocol contents.
type Diagnostics struct{ data bytes.Buffer }

func (d *Diagnostics) Write(data []byte) (int, error) {
	remaining := 16384 - d.data.Len()
	if remaining > 0 {
		_, _ = d.data.Write(data[:min(len(data), remaining)])
	}
	return len(data), nil
}

func (d *Diagnostics) String() string { return d.data.String() }
