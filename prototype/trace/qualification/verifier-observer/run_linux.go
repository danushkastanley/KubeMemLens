package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier"
)

const reorderNanos = uint64(100 * time.Millisecond)

var roles = []string{"enter", "return", "log"}

func sourceBinding(cfg configuration, hashes map[string]string) (binding, error) {
	value := binding{Owner: cfg.Owner, BTF: cfg.BTFSHA256, Executable: cfg.Anchor.SHA256, CgroupInode: cfg.Group.Inode, Formats: map[string]string{}}
	if len(hashes) != 3 {
		return value, errInput
	}
	for _, role := range roles {
		name := "kml_verifier_" + cfg.Owner + "/" + role + "_" + cfg.Owner
		if !hashPattern.MatchString(hashes[name]) {
			return value, errInput
		}
		value.Formats[role] = hashes[name]
	}
	return value, nil
}

func probeCounts(probes *verifier.ProbeSet, owner string) (map[string]verifier.ProbeCount, error) {
	raw, err := probes.Counts()
	if err != nil {
		return nil, err
	}
	if len(raw) != 3 {
		return nil, verifier.ErrObservation
	}
	counts := map[string]verifier.ProbeCount{}
	for _, role := range roles {
		value, exists := raw[role+"_"+owner]
		if !exists || value.Missed != 0 {
			return nil, errors.New("verifier probe coverage incomplete")
		}
		counts[role] = value
	}
	return counts, nil
}

func run(ctx context.Context, cfg configuration, out io.Writer) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cfg.identities(); err != nil {
		return err
	}
	owner, err := bindOwner(cfg)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, owner.close()) }()
	plan, err := verifier.NewProbePlan(cfg.Owner)
	if err != nil {
		return err
	}
	probes, err := verifier.RegisterProbeSet(plan, cfg.BTFSHA256)
	if err != nil {
		return fmt.Errorf("probe setup: %w", err)
	}
	defer func() { result = errors.Join(result, probes.Close()) }()
	_, hashes, err := probes.Formats()
	if err != nil {
		return err
	}
	bound, err := sourceBinding(cfg, hashes)
	if err != nil {
		return err
	}
	cpus, err := cpuList(cfg.OnlineCPUs)
	if err != nil {
		return err
	}
	capture, err := verifier.OpenCapture(probes, owner.group, cfg.Group.Inode, cpus)
	if err != nil {
		return fmt.Errorf("capture setup: %w", err)
	}
	defer func() { result = errors.Join(result, capture.Close()) }()
	merge, err := verifier.NewMerge(cpus)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	written := 0
	for index := 0; index <= cfg.Seconds; {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if err := owner.alive(); err != nil {
			return err
		}
		begin, err := clock()
		if err != nil {
			return err
		}
		if err := capture.Drain(merge.Push); err != nil {
			return fmt.Errorf("record drain: %w", err)
		}
		cutoff := capture.Started() + uint64(index)*uint64(time.Second)
		if begin < capture.Started()+reorderNanos {
			continue
		}
		if begin < cutoff+reorderNanos {
			if _, err := merge.Snapshot(begin - reorderNanos); err != nil {
				return fmt.Errorf("event ordering: %w", err)
			}
			continue
		}
		if begin-cutoff-reorderNanos > uint64(250*time.Millisecond) {
			return errors.New("verifier sample deadline missed")
		}
		var perf verifier.PerfCounters
		var totals verifier.Totals
		if index == cfg.Seconds {
			perf, err = capture.Stop(merge.Push)
			if err == nil {
				totals, err = merge.Finish(cutoff)
			}
			// The protocol leaves an idle tail. A call crossing the end or a
			// record after cutoff cannot silently disappear into a final result.
			if err == nil && perf.Events != totals.CompletedCalls*3+totals.UnassociatedLogs+capture.StartupDiscarded() {
				err = verifier.ErrObservation
			}
		} else {
			perf, err = capture.Counters()
			if err == nil {
				totals, err = merge.Snapshot(cutoff)
			}
		}
		if err != nil {
			return fmt.Errorf("verifier coverage: %w", err)
		}
		counts, err := probeCounts(probes, cfg.Owner)
		if err != nil {
			return err
		}
		if err := cfg.identities(); err != nil {
			return err
		}
		if err := owner.verify(); err != nil {
			return err
		}
		cpu, rss, err := resources()
		if err != nil {
			return err
		}
		end, err := clock()
		if err != nil || end < begin || end-begin > uint64(50*time.Millisecond) {
			return errors.New("verifier observation read exceeded bound")
		}
		row := observation{SchemaVersion: 1, Index: index, Started: capture.Started(), Cutoff: cutoff, ReadStarted: begin, ReadEnded: end,
			CPUUsec: cpu, RSSBytes: rss, StartupDiscarded: capture.StartupDiscarded(), Perf: perf, Observation: totals, Probes: counts, Binding: bound}
		if index == cfg.Seconds {
			row.CloseStarted, err = clock()
			if err != nil {
				return err
			}
			if err := errors.Join(capture.Close(), probes.Close(), owner.close()); err != nil {
				return fmt.Errorf("verifier cleanup: %w", err)
			}
			row.CloseEnded, err = clock()
			if err != nil || row.CloseEnded < row.CloseStarted {
				return verifier.ErrObservation
			}
			row.Closed = true
		}
		if err := writeRow(out, row, &written); err != nil {
			return err
		}
		index++
	}
	return nil
}
