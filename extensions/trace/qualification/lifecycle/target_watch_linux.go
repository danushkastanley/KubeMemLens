package main

import (
	"context"
	"io"
	"slices"
)

// Assignments stay private. Only counts in ascending frozen cgroup-inode order
// leave this helper; the controller retains the input identity bindings.
func targetAssignments(workers []*process, targets map[uint64]bool) ([]uint64, error) {
	result := make([]uint64, 0, len(workers))
	for _, worker := range workers {
		id, err := workerTarget(worker.pid)
		if err != nil || !targets[id] || worker.check() != nil {
			return nil, errOwnership
		}
		result = append(result, id)
	}
	return result, nil
}

func coverageCounts(assignments []uint64, targets map[uint64]bool) ([]int, error) {
	if len(targets) != 2 || len(assignments) > 2 {
		return nil, errOwnership
	}
	ids := make([]uint64, 0, 2)
	for id, selected := range targets {
		if id == 0 || !selected {
			return nil, errOwnership
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	counts := []int{0, 0}
	for _, id := range assignments {
		index, found := slices.BinarySearch(ids, id)
		if !found {
			return nil, errOwnership
		}
		counts[index]++
	}
	return counts, nil
}

func validTargetCoverage(version int, value *snapshot, counts []int) bool {
	if version == 1 {
		return counts == nil
	}
	if version != 2 {
		return false
	}
	if value == nil {
		return counts == nil
	}
	return len(counts) == 2 && counts[0] >= 0 && counts[0] <= 2 && counts[1] >= 0 && counts[1] <= 2 &&
		value.Workers >= 0 && value.Workers <= 2 && counts[0]+counts[1] == value.Workers
}

func ownedTargetObservation(parent *process, digest string, targets map[uint64]bool) (*snapshot, []int, error) {
	if parent.check() != nil {
		return nil, nil, errOwnership
	}
	workers, excluded, err := ownedChildren(parent, digest, targets)
	if err != nil {
		return nil, nil, nil
	}
	before, beforeErr := targetAssignments(workers, targets)
	value, inspectErr := inspectWorkers(workers, excluded)
	after, afterErr := targetAssignments(workers, targets)
	closeErr := closeProcesses(workers)
	if closeErr != nil || parent.check() != nil {
		return nil, nil, errOwnership
	}
	if beforeErr != nil || inspectErr != nil || afterErr != nil || !slices.Equal(before, after) {
		return nil, nil, nil
	}
	counts, err := coverageCounts(after, targets)
	if err != nil {
		return nil, nil, err
	}
	return &value, counts, nil
}

func watchTargetsOwned(ctx context.Context, output io.Writer, parent *process, parentHash, container, digest string, targets map[uint64]bool, seconds int) error {
	if seconds < 1 || seconds > maximumWatchSeconds || len(targets) != 2 {
		return errOwnership
	}
	return watchRecords(ctx, output, seconds, 2, func() (*snapshot, []int, error) {
		checked, err := openProcess(parent.pid, parentHash, parent.start)
		if err != nil {
			return nil, nil, err
		}
		if _, err := checked.containerPath(container); err != nil {
			_ = checked.close()
			return nil, nil, err
		}
		value, counts, err := ownedTargetObservation(checked, digest, targets)
		if checked.close() != nil || parent.check() != nil {
			return nil, nil, errOwnership
		}
		return value, counts, err
	})
}
