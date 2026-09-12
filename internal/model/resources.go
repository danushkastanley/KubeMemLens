package model

import (
	"fmt"
	"time"
)

// ResourceValue describes a value reported by a particular Kubernetes field.
// An absent configured value is unset; an absent status value is unreported.
// Neither is an observed zero or a cgroup enforcement measurement.
type ResourceValue struct {
	Bytes uint64 `json:"bytes"`
	Known bool   `json:"known"`
}

type MemoryResourceBudget struct {
	Request ResourceValue `json:"request"`
	Limit   ResourceValue `json:"limit"`
}

type ResizeState string

const (
	ResizeNone       ResizeState = ""
	ResizePending    ResizeState = "pending"
	ResizeDeferred   ResizeState = "deferred"
	ResizeInfeasible ResizeState = "infeasible"
	ResizeApplying   ResizeState = "in-progress"
	ResizeError      ResizeState = "error"
	ResizeUnknown    ResizeState = "unknown"
)

// Pending and applying are independent: a newer resize may be pending while
// the kubelet is still applying the resources from an earlier generation.
type ResizeObservation struct {
	State              ResizeState  `json:"state"`
	Source             ResizeSource `json:"source,omitempty"`
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
	TransitionAt       time.Time    `json:"transitionAt,omitzero"`
}

type ResizeSource string

const (
	ResizePodCondition ResizeSource = "pod-condition"
	ResizeLegacyStatus ResizeSource = "legacy-status"
)

type PodMemoryResources struct {
	Configured         MemoryResourceBudget `json:"configured"`
	AllocatedRequest   ResourceValue        `json:"allocatedRequest"`
	Applied            MemoryResourceBudget `json:"applied"`
	Generation         int64                `json:"generation,omitempty"`
	ObservedGeneration int64                `json:"observedGeneration,omitempty"`
	Pending            ResizeObservation    `json:"pending,omitzero"`
	Applying           ResizeObservation    `json:"applying,omitzero"`
}

// Value ownership keeps Pod metadata immutable when a container snapshot is
// copied by the collector, the comparison path or incident redaction.
type ContainerMemoryResources struct {
	Pod              PodMemoryResources   `json:"pod"`
	AllocatedRequest ResourceValue        `json:"allocatedRequest"`
	Applied          MemoryResourceBudget `json:"applied"`
}

func (p PodMemoryResources) IsZero() bool { return p == (PodMemoryResources{}) }

func (c ContainerMemoryResources) IsZero() bool { return c == (ContainerMemoryResources{}) }

func (c ContainerMemoryResources) Validate() error {
	for _, value := range []ResourceValue{
		c.Pod.Configured.Request, c.Pod.Configured.Limit, c.Pod.AllocatedRequest,
		c.Pod.Applied.Request, c.Pod.Applied.Limit, c.AllocatedRequest,
		c.Applied.Request, c.Applied.Limit,
	} {
		if !value.Known && value.Bytes != 0 {
			return fmt.Errorf("unreported memory resource has a value")
		}
	}
	if c.Pod.Generation < 0 || c.Pod.ObservedGeneration < 0 {
		return fmt.Errorf("memory resource generation must not be negative")
	}
	if err := validateResizeObservation(c.Pod.Pending, resizePendingTrack); err != nil {
		return err
	}
	return validateResizeObservation(c.Pod.Applying, resizeApplyingTrack)
}

type resizeTrack int

const (
	resizePendingTrack resizeTrack = iota
	resizeApplyingTrack
)

func validateResizeObservation(observation ResizeObservation, track resizeTrack) error {
	if observation.ObservedGeneration < 0 {
		return fmt.Errorf("resize generation must not be negative")
	}
	switch observation.State {
	case ResizeNone:
		if observation.Source != "" || observation.ObservedGeneration != 0 || !observation.TransitionAt.IsZero() {
			return fmt.Errorf("absent resize state has observation metadata")
		}
	case ResizeUnknown:
	case ResizePending, ResizeDeferred, ResizeInfeasible:
		if track != resizePendingTrack {
			return fmt.Errorf("pending resize state used for application")
		}
	case ResizeApplying, ResizeError:
		if track != resizeApplyingTrack {
			return fmt.Errorf("application resize state used for allocation")
		}
	default:
		return fmt.Errorf("invalid resize state")
	}
	if observation.State != ResizeNone && observation.Source != ResizePodCondition && observation.Source != ResizeLegacyStatus {
		return fmt.Errorf("invalid resize source")
	}
	return nil
}
