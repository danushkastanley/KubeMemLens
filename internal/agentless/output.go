package agentless

import (
	"encoding/json"

	"github.com/danushkastanley/kube-memlens/internal/observation"
)

// Check the exact JSON size in bounded pieces. Marshaling a whole batch merely
// to check its size would allocate the potentially oversized result first.
func checkOutputSize(batch observation.Batch, limit int64) error {
	header := batch
	header.Pods, header.Nodes, header.Namespaces, header.Workloads = nil, nil, nil, nil
	size, err := jsonSize(header)
	if err != nil {
		return err
	}
	size -= 4 * 4 // Replace the four JSON nulls with measured arrays below.
	add := func(length int64, err error) error {
		if err != nil {
			return err
		}
		size += length
		if size > limit {
			return queryFailure(limitReached, nil)
		}
		return nil
	}
	if err := add(arraySize(batch.Pods, limit-size, podJSONSize)); err != nil {
		return err
	}
	if err := add(arraySize(batch.Nodes, limit-size, jsonSize[observation.Node])); err != nil {
		return err
	}
	if err := add(arraySize(batch.Namespaces, limit-size, jsonSize[observation.Group])); err != nil {
		return err
	}
	return add(arraySize(batch.Workloads, limit-size, jsonSize[observation.Group]))
}

func jsonSize[T any](value T) (int64, error) {
	encoded, err := json.Marshal(value)
	return int64(len(encoded)), err
}

func podJSONSize(pod observation.Pod) (int64, error) {
	containers := pod.Containers
	pod.Containers = nil
	size, err := jsonSize(pod)
	if err != nil {
		return 0, err
	}
	// Each container has bounded text and no copied Pod label map. A Pod header
	// is bounded by its original API response, independent of container count.
	containerSize, err := arraySize(containers, 64<<20, jsonSize[observation.Container])
	return size - 4 + containerSize, err
}

func arraySize[T any](values []T, limit int64, measure func(T) (int64, error)) (int64, error) {
	if values == nil {
		return 4, nil
	}
	size := int64(2)
	for i, value := range values {
		length, err := measure(value)
		if err != nil {
			return 0, err
		}
		size += length
		if i > 0 {
			size++
		}
		if size > limit {
			return 0, queryFailure(limitReached, nil)
		}
	}
	return size, nil
}
