package sdk

import (
	"sync/atomic"

	"github.com/inspektor-gadget/inspektor-gadget/pkg/logger"
)

// SDK messages may contain runtime identifiers. Retain only severity state; the
// worker must surface failure/incompleteness through its fixed result contract.
type diagnostics struct {
	failed atomic.Bool
	warned atomic.Bool
}

func (d *diagnostics) Log(level logger.Level, _ ...any) {
	if level <= logger.ErrorLevel {
		d.failed.Store(true)
	}
	if level == logger.WarnLevel {
		d.warned.Store(true)
	}
}
func (d *diagnostics) Logf(level logger.Level, _ string, _ ...any) { d.Log(level) }
func (*diagnostics) SetLevel(logger.Level)                         {}
func (*diagnostics) GetLevel() logger.Level                        { return logger.WarnLevel }
