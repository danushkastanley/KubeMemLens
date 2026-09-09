package model

import (
	"testing"
	"time"
)

func TestEffectivePodMemoryKeepsPodAndContainerBudgetsSeparate(t *testing.T) {
	containers := ContainerResourceTotals{RequestBytes: 128, LimitBytes: 256, RequestContainers: 2, LimitContainers: 2, Containers: 2}
	for _, tc := range []struct {
		name string
		pod  MemoryResourceBudget
		want EffectiveMemoryResources
	}{
		{"container-only", MemoryResourceBudget{}, EffectiveMemoryResources{
			Request: EffectiveResourceValue{128, BudgetContainers}, Limit: EffectiveResourceValue{256, BudgetContainers},
		}},
		{"shared Pod budget", MemoryResourceBudget{Request: ResourceValue{192, true}, Limit: ResourceValue{384, true}}, EffectiveMemoryResources{
			Request: EffectiveResourceValue{192, BudgetPod}, Limit: EffectiveResourceValue{384, BudgetPod},
		}},
		{"Pod request only", MemoryResourceBudget{Request: ResourceValue{192, true}}, EffectiveMemoryResources{
			Request: EffectiveResourceValue{192, BudgetPod}, Limit: EffectiveResourceValue{256, BudgetContainers},
		}},
		{"Pod limit only", MemoryResourceBudget{Limit: ResourceValue{384, true}}, EffectiveMemoryResources{
			Request: EffectiveResourceValue{128, BudgetContainers}, Limit: EffectiveResourceValue{384, BudgetPod},
		}},
		{"explicit zero request", MemoryResourceBudget{Request: ResourceValue{0, true}}, EffectiveMemoryResources{
			Request: EffectiveResourceValue{0, BudgetPod}, Limit: EffectiveResourceValue{256, BudgetContainers},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := PodMemoryResources{Configured: tc.pod, Applied: MemoryResourceBudget{Limit: ResourceValue{99, true}}}
			if got := EffectivePodMemory(pod, containers); got != tc.want {
				t.Fatalf("effective budget = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEffectivePodMemoryPreservesIncompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		totals ContainerResourceTotals
		want   ResourceBudgetSource
	}{
		{"no containers", ContainerResourceTotals{}, BudgetUnreported},
		{"unset", ContainerResourceTotals{Containers: 2}, BudgetUnset},
		{"partial", ContainerResourceTotals{RequestBytes: 64, RequestContainers: 1, Containers: 2}, BudgetPartial},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EffectivePodMemory(PodMemoryResources{}, tc.totals)
			if got.Request.Source != tc.want || got.Request.Bytes != tc.totals.RequestBytes {
				t.Fatalf("resource evidence changed: %+v", got.Request)
			}
		})
	}
}

func TestMemoryResourceValidationKeepsResizeTracksIndependent(t *testing.T) {
	resource := ContainerMemoryResources{Pod: PodMemoryResources{
		Generation: 3, ObservedGeneration: 2,
		Pending:  ResizeObservation{State: ResizeDeferred, Source: ResizePodCondition, ObservedGeneration: 3},
		Applying: ResizeObservation{State: ResizeApplying, Source: ResizePodCondition, ObservedGeneration: 2},
	}}
	if err := resource.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ContainerMemoryResources){
		func(c *ContainerMemoryResources) { c.Pod.Configured.Request = ResourceValue{Bytes: 100} },
		func(c *ContainerMemoryResources) { c.Pod.Generation = -1 },
		func(c *ContainerMemoryResources) { c.Pod.Pending.State = ResizeApplying },
		func(c *ContainerMemoryResources) { c.Pod.Applying.State = ResizeDeferred },
		func(c *ContainerMemoryResources) { c.Pod.Pending.State = "unbounded raw message" },
		func(c *ContainerMemoryResources) { c.Pod.Pending.ObservedGeneration = -1 },
		func(c *ContainerMemoryResources) { c.Pod.Applying = ResizeObservation{TransitionAt: time.Unix(100, 0)} },
		func(c *ContainerMemoryResources) { c.Pod.Pending.Source = "untrusted-source" },
	} {
		invalid := resource
		mutate(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid resource context accepted: %+v", invalid)
		}
	}
}
