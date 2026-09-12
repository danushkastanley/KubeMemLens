package nodestats

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

type VolumeStatsMode string

const (
	VolumeStatsDisabled VolumeStatsMode = ""
	VolumeStatsEnabled  VolumeStatsMode = "enabled"
)

// Sample separates private volume records from Node memory. Read preserves
// the existing memory-only interface; publishers use ReadSample instead.
type Sample struct {
	Node    nodecontext.Observation `json:"node"`
	Volumes *volumecontext.Batch    `json:"-"`
}

type SampleSource interface {
	ReadSample(context.Context) (Sample, error)
}

func (s *Source) volumeBatch(ctx context.Context, data []byte, nodeName, nodeUID string, at time.Time) (*volumecontext.Batch, error) {
	records, omitted, err := decodeVolumes(ctx, data, nodeUID)
	state := volumecontext.SourceState(volumehealth.Reported, "")
	batch, validationErr := volumecontext.NewBatch(nodeName, nodeUID, at, state, records, at)
	if err != nil || validationErr != nil {
		s.opts.Telemetry.recordVolumes(0, omitted, errors.Join(err, validationErr))
		// Identity and report time came from the successful Node source. A
		// volume failure cannot erase or make that Node observation fail.
		batch, validationErr = volumecontext.NewBatch(nodeName, nodeUID, at,
			volumecontext.SourceState(volumehealth.Unavailable, volumecontext.SourceFailed), nil, at)
		if validationErr != nil {
			return nil, &Error{Reason: nodecontext.InvalidTarget}
		}
		return &batch, nil
	}
	s.opts.Telemetry.recordVolumes(len(records), omitted, nil)
	return &batch, nil
}

// decodeVolumes reads the same bounded response as the Node decoder. A second
// bounded pass isolates malformed volume fields from valid Node memory.
func decodeVolumes(ctx context.Context, data []byte, nodeUID string) ([]volumecontext.RawUsage, int, error) {
	if len(data) > nodecontext.MaxSummaryBytes {
		return nil, 0, errJSON
	}
	d := newDecoder(ctx, data)
	rows := []volumecontext.RawUsage{}
	omitted, scanned := 0, 0
	err := d.object(map[string]func() error{
		"pods": func() error {
			return d.array(func(index int) error {
				if index >= volumecontext.MaxBatchRecords {
					return errJSON
				}
				podRows, podOmitted, err := d.podVolumes(nodeUID, &scanned)
				if err != nil {
					return err
				}
				rows = append(rows, podRows...)
				omitted += podOmitted
				return nil
			})
		},
	})
	if err != nil || d.finish() != nil {
		return nil, omitted, errJSON
	}
	return rows, omitted, nil
}

func (d *decoder) podVolumes(nodeUID string, scanned *int) ([]volumecontext.RawUsage, int, error) {
	var namespace, uid string
	rows := []volumecontext.RawUsage{}
	omitted := 0
	seen := map[string]bool{}
	err := d.object(map[string]func() error{
		"podRef": func() error {
			return d.object(map[string]func() error{
				"namespace": func() error { return d.stringInto(&namespace, 63) },
				"uid":       func() error { return d.stringInto(&uid, volumecontext.MaxUIDBytes) },
			})
		},
		"volume": func() error {
			return d.array(func(index int) error {
				*scanned = *scanned + 1
				if index >= volumecontext.MaxVolumesPerPod || *scanned > volumecontext.MaxBatchRecords {
					return errJSON
				}
				row, err := d.volumeFilesystem()
				if err != nil {
					return err
				}
				if row.VolumeName != "" && seen[row.VolumeName] {
					return errJSON
				}
				seen[row.VolumeName] = true
				if row.VolumeName == "" || !filesystemReported(row.Filesystem) {
					omitted++
					return nil
				}
				rows = append(rows, row)
				return nil
			})
		},
	})
	if err != nil {
		return nil, omitted, err
	}
	for i := range rows {
		rows[i].Namespace, rows[i].PodUID, rows[i].NodeUID = namespace, uid, nodeUID
	}
	return rows, omitted, nil
}

func (d *decoder) volumeFilesystem() (volumecontext.RawUsage, error) {
	row := volumecontext.RawUsage{}
	f := &row.Filesystem
	err := d.object(map[string]func() error{
		"name": func() error { return d.stringInto(&row.VolumeName, 63) },
		"pvcRef": func() error {
			if absent, err := d.absent(); absent || err != nil {
				return err
			}
			return d.object(map[string]func() error{
				"namespace": func() error { return d.stringInto(&row.PVCNamespace, 63) },
				"name":      func() error { return d.stringInto(&row.PVCName, volumecontext.MaxNameBytes) },
			})
		},
		"time":           func() error { return d.timeInto(&f.CapturedAt) },
		"capacityBytes":  func() error { return d.uintInto(&f.CapacityBytes) },
		"usedBytes":      func() error { return d.uintInto(&f.UsedBytes) },
		"availableBytes": func() error { return d.uintInto(&f.AvailableBytes) },
		"inodes":         func() error { return d.uintInto(&f.Inodes) },
		"inodesUsed":     func() error { return d.uintInto(&f.InodesUsed) },
		"inodesFree":     func() error { return d.uintInto(&f.InodesFree) },
	})
	return row, err
}

func filesystemReported(f volumecontext.Filesystem) bool {
	return f.CapacityBytes != nil || f.UsedBytes != nil || f.AvailableBytes != nil || f.Inodes != nil || f.InodesUsed != nil || f.InodesFree != nil
}
