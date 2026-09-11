package incident

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"k8s.io/apimachinery/pkg/util/validation"
)

func ValidateRestricted(bundle RestrictedBundle) error {
	b := bundle.Observations
	if bundle.SchemaVersion != RestrictedSchemaVersion || b.Mode != capability.Restricted {
		return fmt.Errorf("incident schema 3 requires restricted observations")
	}
	if bundle.CapturedAt.IsZero() || b.ReceivedAt.IsZero() || b.ReceivedAt.After(bundle.CapturedAt) {
		return fmt.Errorf("incident capture and receive times are invalid")
	}
	if err := validateScalars(reflect.ValueOf(bundle)); err != nil {
		return err
	}
	if !complete(b.Completeness) || len(b.Sources) == 0 || len(b.Sources) > 16 || len(b.Pods) > 2000 || len(b.Nodes) > 500 || len(b.Namespaces) > 2000 || len(b.Workloads) > 2000 || len(b.Caveats) > 128 {
		return fmt.Errorf("restricted incident has invalid completeness or entity bounds")
	}
	seen := map[string]bool{}
	for _, source := range b.Sources {
		if b.Completeness == capability.Complete && source.Completeness != capability.Complete {
			return fmt.Errorf("partial source cannot produce a complete incident")
		}
		key := string(source.Source) + "/" + string(source.Scope)
		if seen[key] || (source.Scope != capability.PodScope && source.Scope != capability.NodeScope) || !availability(source.Availability) {
			return fmt.Errorf("invalid or duplicate restricted source report")
		}
		seen[key] = true
		if err := validateEnvelope(capability.Envelope{Source: source.Source, APIVersion: source.APIVersion, Scope: source.Scope, Freshness: source.Freshness, Completeness: source.Completeness, Stability: source.Stability}, source.Source, source.Scope); err != nil {
			return err
		}
	}
	containers := 0
	seen = map[string]bool{}
	for _, pod := range b.Pods {
		if b.Completeness == capability.Complete && (pod.WorkingSet.Evidence.Completeness != capability.Complete || pod.StatusEvidence.Completeness != capability.Complete || pod.OwnerEvidence.Completeness != capability.Complete) {
			return fmt.Errorf("partial Pod evidence cannot produce a complete incident")
		}
		key := pod.Namespace + "/" + pod.Name
		if !dns(pod.Namespace) || !dns(pod.Name) || seen[key] || pod.Cgroup != nil || !availability(pod.OwnerAvailability) {
			return fmt.Errorf("invalid restricted Pod identity or evidence")
		}
		seen[key] = true
		if bundle.Redacted && (pod.UID != "" || len(pod.Context.Labels) > 0) {
			return fmt.Errorf("redacted incident contains Pod identifiers or labels")
		}
		if err := (model.ContainerMemoryResources{Pod: pod.Context.Resources}).Validate(); err != nil {
			return err
		}
		if err := validateEnvelope(pod.StatusEvidence, capability.KubernetesStatus, capability.PodScope); err != nil {
			return err
		}
		if pod.OwnerEvidence.Scope != capability.PodScope && pod.OwnerEvidence.Scope != capability.WorkloadScope {
			return fmt.Errorf("invalid owner evidence scope")
		}
		if err := validateEnvelope(pod.OwnerEvidence, capability.KubernetesStatus, pod.OwnerEvidence.Scope); err != nil {
			return err
		}
		if err := validateSet(pod.WorkingSet, capability.PodScope); err != nil {
			return err
		}
		values := make([]observation.WorkingSet, 0, len(pod.Containers))
		names := map[string]bool{}
		for _, c := range pod.Containers {
			containers++
			if containers > 10000 || !dns(c.Name) || names[c.Name] || c.Cgroup != nil {
				return fmt.Errorf("invalid restricted container identity or bounds")
			}
			names[c.Name] = true
			if bundle.Redacted && len(c.Context.Labels) > 0 {
				return fmt.Errorf("redacted incident contains container labels")
			}
			if err := c.Context.Resources.Validate(); err != nil {
				return err
			}
			if err := validateSet(c.WorkingSet, capability.ContainerScope); err != nil {
				return err
			}
			values = append(values, c.WorkingSet)
		}
		if !sameAggregate(pod.WorkingSet, observation.SumWorkingSets(values, capability.PodScope)) {
			return fmt.Errorf("Pod working set does not match container coverage and sum")
		}
	}
	seen = map[string]bool{}
	for _, node := range b.Nodes {
		if !dns(node.Name) || seen[node.Name] || node.DeepStatus != nil || !availability(node.StatusAvailability) {
			return fmt.Errorf("invalid restricted Node identity or evidence")
		}
		seen[node.Name] = true
		if !oneOf(node.MemoryPressure, "healthy", "adverse", "unknown", "unreported") || (node.StatusAvailability != capability.Available && node.MemoryPressure != "unreported") {
			return fmt.Errorf("Node pressure requires available status evidence")
		}
		if bundle.Redacted && node.UID != "" {
			return fmt.Errorf("redacted incident contains Node UID")
		}
		if err := validateEnvelope(node.StatusEvidence, capability.KubernetesStatus, capability.NodeScope); err != nil {
			return err
		}
		if err := validateSet(node.WorkingSet, capability.NodeScope); err != nil {
			return err
		}
	}
	namespaces, workloads := observation.GroupPods(b.Pods)
	if err := validateGroups(b.Namespaces, namespaces, capability.NamespaceScope); err != nil {
		return err
	}
	return validateGroups(b.Workloads, workloads, capability.WorkloadScope)
}

func validateGroups(groups, expected []observation.Group, scope capability.Scope) error {
	if len(groups) != len(expected) {
		return fmt.Errorf("restricted groups do not match captured Pods")
	}
	byKey := map[string]observation.Group{}
	for _, group := range expected {
		byKey[group.Namespace+"/"+group.Kind+"/"+group.Name] = group
	}
	for _, group := range groups {
		key := group.Namespace + "/" + group.Kind + "/" + group.Name
		want, ok := byKey[key]
		if !ok || group.Cgroup != nil || group.PodCount != want.PodCount || !sameAggregate(group.WorkingSet, want.WorkingSet) {
			return fmt.Errorf("restricted group sum or coverage does not match captured Pods")
		}
		delete(byKey, key)
		if err := validateSet(group.WorkingSet, scope); err != nil {
			return err
		}
	}
	return nil
}

func sameAggregate(a, b observation.WorkingSet) bool {
	return reflect.DeepEqual(a.Bytes, b.Bytes) && a.Coverage == b.Coverage && a.Evidence.Source == b.Evidence.Source && a.Evidence.APIVersion == b.Evidence.APIVersion && a.Evidence.CapturedAt.Equal(b.Evidence.CapturedAt) && a.LatestSampleAt.Equal(b.LatestSampleAt) && (a.Evidence.Completeness != capability.Complete || b.Evidence.Completeness == capability.Complete)
}

func validateSet(v observation.WorkingSet, scope capability.Scope) error {
	if err := validateEnvelope(v.Evidence, capability.KubernetesMetrics, scope); err != nil {
		return err
	}
	if !availability(v.Availability) || v.Coverage.Expected < 0 || v.Coverage.Expected > 10000 || v.Coverage.Reported < 0 || v.Coverage.Reported > v.Coverage.Expected {
		return fmt.Errorf("invalid working-set coverage or availability")
	}
	unit := capability.ContainerScope
	if scope == capability.NodeScope {
		unit = capability.NodeScope
	}
	if v.Coverage.Unit != unit {
		return fmt.Errorf("invalid working-set coverage unit")
	}
	if v.Bytes == nil {
		if v.Availability == capability.Available || v.Evidence.Completeness == capability.Complete {
			return fmt.Errorf("missing working set cannot be available or complete")
		}
		return nil
	}
	if v.Availability != capability.Available || v.Evidence.CapturedAt.IsZero() || v.Evidence.ReceivedAt.IsZero() || v.Coverage.Reported == 0 || v.Evidence.APIVersion == "" {
		return fmt.Errorf("measured working set requires source, times and reported coverage")
	}
	if v.Evidence.Completeness == capability.Complete && v.Coverage.Reported != v.Coverage.Expected {
		return fmt.Errorf("incomplete coverage cannot be complete")
	}
	if !v.LatestSampleAt.IsZero() && v.LatestSampleAt.Before(v.Evidence.CapturedAt) {
		return fmt.Errorf("working-set sample range is reversed")
	}
	if (scope == capability.ContainerScope || scope == capability.NodeScope) && (v.Coverage.Expected != 1 || v.Evidence.Window <= 0) {
		return fmt.Errorf("individual working set requires one sample and a positive window")
	}
	return nil
}

func validateEnvelope(e capability.Envelope, source capability.Source, scope capability.Scope) error {
	if (source != capability.KubernetesMetrics && source != capability.KubernetesStatus) || e.Source != source || e.Scope != scope || !complete(e.Completeness) || e.Window < 0 || len(e.Caveats) > 128 {
		return fmt.Errorf("invalid restricted source, scope or completeness")
	}
	if !oneOf(string(e.Freshness), "fresh", "stale", "rebuilding", "unavailable") || !oneOf(string(e.Stability), "", "stable", "beta", "alpha", "implementation-specific") {
		return fmt.Errorf("invalid evidence freshness or capability stability")
	}
	versions := []string{"", "v1", "apps/v1", "batch/v1"}
	if source == capability.KubernetesMetrics {
		versions = []string{"", "metrics.k8s.io/v1", "metrics.k8s.io/v1beta1"}
	}
	if !oneOf(e.APIVersion, versions...) {
		return fmt.Errorf("invalid restricted source API version")
	}
	return nil
}

func complete(value capability.Completeness) bool {
	return value == capability.Complete || value == capability.Partial
}
func availability(value capability.Availability) bool {
	return oneOf(string(value), "available", "forbidden", "absent", "unsupported", "disabled", "unreported", "unavailable")
}
func dns(value string) bool { return len(validation.IsDNS1123Subdomain(value)) == 0 }
func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

// Captures are an untrusted text boundary too: reject terminal controls and
// bound nested metadata rather than allowing them into CLI or TUI renderers.
func validateScalars(v reflect.Value) error {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return validateScalars(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := validateScalars(v.Field(i)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v.Len() > 10000 {
			return fmt.Errorf("incident nested entity limit exceeded")
		}
		for i := 0; i < v.Len(); i++ {
			if err := validateScalars(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		if v.Len() > 256 {
			return fmt.Errorf("incident metadata limit exceeded")
		}
		for _, key := range v.MapKeys() {
			if err := validateScalars(key); err != nil {
				return err
			}
			if err := validateScalars(v.MapIndex(key)); err != nil {
				return err
			}
		}
	case reflect.String:
		text := v.String()
		if len(text) > 4096 || strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
			return fmt.Errorf("incident text contains controls or exceeds its length limit")
		}
	case reflect.Uint64:
		if v.Uint() > math.MaxInt64 {
			return fmt.Errorf("incident memory quantity exceeds supported range")
		}
	}
	return nil
}
