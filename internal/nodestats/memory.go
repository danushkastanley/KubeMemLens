package nodestats

import (
	"encoding/json"
	"math"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func (d *decoder) memory() (*nodecontext.Memory, error) {
	if absent, err := d.absent(); absent || err != nil {
		return nil, err
	}
	value := &nodecontext.Memory{}
	err := d.object(map[string]func() error{
		"time":            func() error { return d.timeInto(&value.CapturedAt) },
		"availableBytes":  func() error { return d.uintInto(&value.AvailableBytes) },
		"usageBytes":      func() error { return d.uintInto(&value.UsageBytes) },
		"workingSetBytes": func() error { return d.uintInto(&value.WorkingSetBytes) },
		"rssBytes":        func() error { return d.uintInto(&value.RSSBytes) },
		"pageFaults":      func() error { return d.uintInto(&value.PageFaults) },
		"majorPageFaults": func() error { return d.uintInto(&value.MajorPageFaults) },
		"psi": func() error {
			psi, err := d.psi()
			value.PSI = psi
			return err
		},
	})
	if err != nil || value.CapturedAt.IsZero() {
		return nil, errJSON
	}
	if value.UsageBytes != nil && value.WorkingSetBytes != nil && *value.WorkingSetBytes > *value.UsageBytes {
		return nil, errJSON
	}
	return value, nil
}

func (d *decoder) swap() (*nodecontext.Swap, error) {
	if absent, err := d.absent(); absent || err != nil {
		return nil, err
	}
	value := &nodecontext.Swap{}
	err := d.object(map[string]func() error{
		"time":               func() error { return d.timeInto(&value.CapturedAt) },
		"swapUsageBytes":     func() error { return d.uintInto(&value.UsageBytes) },
		"swapAvailableBytes": func() error { return d.uintInto(&value.AvailableBytes) },
	})
	if err != nil || value.CapturedAt.IsZero() {
		return nil, errJSON
	}
	return value, nil
}

func (d *decoder) psi() (*nodecontext.PSI, error) {
	if absent, err := d.absent(); absent || err != nil {
		return nil, err
	}
	value := &nodecontext.PSI{}
	var some, full bool
	err := d.object(map[string]func() error{
		"some": func() error { some = true; return d.psiData(&value.Some) },
		"full": func() error { full = true; return d.psiData(&value.Full) },
	})
	if err != nil || !some || !full {
		return nil, errJSON
	}
	return value, nil
}

func (d *decoder) psiData(value *nodecontext.PSIData) error {
	var total *uint64
	seen := 0
	err := d.object(map[string]func() error{
		"total":  func() error { return d.uintInto(&total) },
		"avg10":  func() error { seen++; return d.percentInto(&value.Avg10) },
		"avg60":  func() error { seen++; return d.percentInto(&value.Avg60) },
		"avg300": func() error { seen++; return d.percentInto(&value.Avg300) },
	})
	if err != nil || total == nil || seen != 3 {
		return errJSON
	}
	value.TotalNanoseconds = *total
	return nil
}

func (d *decoder) percentInto(target *float64) error {
	token, err := d.next()
	number, ok := token.(json.Number)
	if err != nil || !ok {
		return errJSON
	}
	value, err := strconv.ParseFloat(string(number), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
		return errJSON
	}
	*target = value
	return nil
}
