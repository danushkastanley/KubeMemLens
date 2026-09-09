package kube

import (
	"github.com/danushkastanley/kube-memlens/internal/model"
	corev1 "k8s.io/api/core/v1"
)

func memoryResourceContext(pod corev1.Pod, status corev1.ContainerStatus) model.ContainerMemoryResources {
	pending, applying := podResizeObservations(pod.Status)
	resources := model.ContainerMemoryResources{
		Pod: model.PodMemoryResources{
			Configured:       memoryResourceBudget(pod.Spec.Resources),
			AllocatedRequest: memoryResourceValue(pod.Status.AllocatedResources),
			Applied:          memoryResourceBudget(pod.Status.Resources),
			Pending:          pending, Applying: applying,
		},
		AllocatedRequest: memoryResourceValue(status.AllocatedResources),
		Applied:          memoryResourceBudget(status.Resources),
	}
	if !needsMemoryResourceContext(resources, configuredContainerMemory(pod, status.Name)) {
		return model.ContainerMemoryResources{}
	}
	resources.Pod.Generation = pod.Generation
	resources.Pod.ObservedGeneration = pod.Status.ObservedGeneration
	return resources
}

func configuredContainerMemory(pod corev1.Pod, name string) *model.MemoryResourceBudget {
	resources, found := containerResources(pod, name)
	if !found {
		return nil
	}
	budget := memoryResourceBudget(&resources)
	return &budget
}

func needsMemoryResourceContext(resources model.ContainerMemoryResources, configured *model.MemoryResourceBudget) bool {
	if resources.Pod.Configured.Request.Known || resources.Pod.Configured.Limit.Known {
		return true
	}
	if resources.Pod.Pending.State != model.ResizeNone || resources.Pod.Applying.State != model.ResizeNone {
		return true
	}
	if configured == nil {
		return false
	}
	return resourceValueChanged(configured.Request, resources.AllocatedRequest) ||
		resourceValueChanged(configured.Request, resources.Applied.Request) ||
		resourceValueChanged(configured.Limit, resources.Applied.Limit)
}

func resourceValueChanged(configured, reported model.ResourceValue) bool {
	return reported.Known && (!configured.Known || configured.Bytes != reported.Bytes)
}

func memoryResourceBudget(resources *corev1.ResourceRequirements) model.MemoryResourceBudget {
	if resources == nil {
		return model.MemoryResourceBudget{}
	}
	return model.MemoryResourceBudget{
		Request: memoryResourceValue(resources.Requests),
		Limit:   memoryResourceValue(resources.Limits),
	}
}

func memoryResourceValue(resources corev1.ResourceList) model.ResourceValue {
	quantity, present := resources[corev1.ResourceMemory]
	if !present {
		return model.ResourceValue{}
	}
	return model.ResourceValue{Bytes: quantityBytes(quantity), Known: true}
}

func podResizeObservations(status corev1.PodStatus) (pending, applying model.ResizeObservation) {
	modern := false
	for _, condition := range status.Conditions {
		switch condition.Type {
		case corev1.PodResizePending:
			modern = true
			pending = resizeObservation(condition, pendingResizeState(condition))
		case corev1.PodResizeInProgress:
			modern = true
			applying = resizeObservation(condition, applyingResizeState(condition))
		}
	}
	if modern {
		return pending, applying
	}
	legacy := model.ResizeObservation{Source: model.ResizeLegacyStatus}
	switch status.Resize {
	case "":
		return pending, applying
	case corev1.PodResizeStatusInProgress:
		legacy.State = model.ResizeApplying
		return pending, legacy
	case corev1.PodResizeStatusDeferred:
		legacy.State = model.ResizeDeferred
	case corev1.PodResizeStatusInfeasible:
		legacy.State = model.ResizeInfeasible
	default:
		legacy.State = model.ResizeUnknown
	}
	return legacy, applying
}

func resizeObservation(condition corev1.PodCondition, state model.ResizeState) model.ResizeObservation {
	if condition.Status == corev1.ConditionFalse {
		return model.ResizeObservation{}
	}
	if condition.Status != corev1.ConditionTrue {
		state = model.ResizeUnknown
	}
	return model.ResizeObservation{
		State: state, Source: model.ResizePodCondition,
		ObservedGeneration: condition.ObservedGeneration,
		TransitionAt:       condition.LastTransitionTime.Time.UTC(),
	}
}

func pendingResizeState(condition corev1.PodCondition) model.ResizeState {
	switch condition.Reason {
	case "":
		return model.ResizePending
	case corev1.PodReasonDeferred:
		return model.ResizeDeferred
	case corev1.PodReasonInfeasible:
		return model.ResizeInfeasible
	default:
		return model.ResizeUnknown
	}
}

func applyingResizeState(condition corev1.PodCondition) model.ResizeState {
	switch condition.Reason {
	case "":
		return model.ResizeApplying
	case corev1.PodReasonError:
		return model.ResizeError
	default:
		return model.ResizeUnknown
	}
}
