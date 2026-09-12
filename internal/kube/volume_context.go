package kube

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

type VolumeAccess struct{ Group, Resource, Namespace, Name string }
type VolumeAuthorizer func(context.Context, VolumeAccess) error
type VolumeNodeIdentity func(string, time.Time) (string, bool)

type ResolvedVolumes struct {
	Scope    volumecontext.PodScope
	Bindings []volumecontext.Binding
}

type VolumeResolver interface {
	Resolve(context.Context, string, string) (ResolvedVolumes, error)
}

var ErrVolumePodNotFound = errors.New("selected Pod was not found")

type volumeResolver struct {
	reader    *volumeHealthReader
	authorize VolumeAuthorizer
	nodeUID   VolumeNodeIdentity
}

// NewVolumeResolver uses the collector's explicitly granted acquisition rights.
// Every disclosed object additionally requires the original caller's rights.
func NewVolumeResolver(config *rest.Config, authorize VolumeAuthorizer, nodeUID VolumeNodeIdentity) (VolumeResolver, error) {
	if config == nil || config.Insecure || !strings.HasPrefix(config.Host, "https://") || authorize == nil || nodeUID == nil {
		return nil, errors.New("volume resolver requires verified transport and caller authorisation")
	}
	reader, err := newVolumeHealthReader(config, VolumeHealthOptions{Namespace: "default", Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	return &volumeResolver{reader: reader, authorize: authorize, nodeUID: nodeUID}, nil
}

func (r *volumeResolver) Resolve(ctx context.Context, namespace, podName string) (ResolvedVolumes, error) {
	if len(validation.IsDNS1123Label(namespace)) != 0 || !validHealthName(podName) {
		return ResolvedVolumes{}, invalidHealth()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := volumeBindingQuery{healthQuery: healthQuery{reader: r.reader, remaining: maxHealthQueryBytes, validateJSON: boundedVolumeObject}, authorize: r.authorize}
	var pod corev1.Pod
	access := VolumeAccess{Resource: "pods", Namespace: namespace, Name: podName}
	path := "/api/v1/namespaces/" + namespace + "/pods/" + podName
	if err := q.authorisedGet(ctx, access, path, &pod); err != nil {
		if errors.Is(err, errHealthObjectNotFound) {
			return ResolvedVolumes{}, ErrVolumePodNotFound
		}
		return ResolvedVolumes{}, err
	}
	if pod.Kind != "Pod" || pod.APIVersion != "v1" || pod.Name != podName || pod.Namespace != namespace || pod.UID == "" || len(pod.UID) > volumecontext.MaxUIDBytes || pod.CreationTimestamp.IsZero() || len(pod.Spec.Volumes) > volumecontext.MaxVolumesPerPod {
		return ResolvedVolumes{}, invalidHealth()
	}
	if pod.Spec.NodeName == "" {
		return ResolvedVolumes{}, &HealthReadError{Reason: volumehealth.NotScheduled}
	}
	uid, known := r.nodeUID(pod.Spec.NodeName, time.Now().UTC())
	if !known {
		return ResolvedVolumes{}, &HealthReadError{Reason: volumehealth.BindingUnavailable}
	}
	result := ResolvedVolumes{Scope: volumecontext.PodScope{Namespace: namespace, PodName: podName, PodUID: string(pod.UID), CreatedAt: pod.CreationTimestamp.Time, NodeName: pod.Spec.NodeName, NodeUID: uid}}
	mounts, err := volumeMountCounts(&pod)
	if err != nil {
		return ResolvedVolumes{}, err
	}
	for _, v := range pod.Spec.Volumes {
		b, err := q.binding(ctx, &pod, v, mounts[v.Name])
		if err != nil {
			return ResolvedVolumes{}, err
		}
		result.Bindings = append(result.Bindings, b)
	}
	// Recheck the Pod after dependent object reads so a replaced/rescheduled
	// target cannot be labelled with the old selection's identity.
	var current corev1.Pod
	if err := q.authorisedGet(ctx, access, path, &current); err != nil {
		if errors.Is(err, errHealthObjectNotFound) {
			return ResolvedVolumes{}, ErrVolumePodNotFound
		}
		return ResolvedVolumes{}, err
	}
	if current.Kind != "Pod" || current.APIVersion != "v1" || current.UID != pod.UID || current.Spec.NodeName != pod.Spec.NodeName || current.Namespace != namespace || current.Name != podName {
		return ResolvedVolumes{}, &HealthReadError{Reason: volumehealth.BindingUnavailable}
	}
	if _, err := volumecontext.Join(result.Scope, result.Bindings, nil, nil, volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport), time.Now().UTC()); err != nil {
		return ResolvedVolumes{}, invalidHealth()
	}
	return result, nil
}

type volumeBindingQuery struct {
	healthQuery
	authorize VolumeAuthorizer
}

func (q *volumeBindingQuery) authorisedGet(ctx context.Context, a VolumeAccess, path string, target any) error {
	if err := q.authorize(ctx, a); err != nil {
		return err
	}
	return q.get(ctx, path, target)
}

type mountCounts struct{ total, readOnly int }

func volumeMountCounts(pod *corev1.Pod) (map[string]mountCounts, error) {
	if len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+len(pod.Spec.EphemeralContainers) > 256 {
		return nil, invalidHealth()
	}
	result := map[string]mountCounts{}
	volumes := map[string]bool{}
	for _, v := range pod.Spec.Volumes {
		if !validHealthName(v.Name) || len(v.Name) > 63 || volumes[v.Name] {
			return nil, invalidHealth()
		}
		volumes[v.Name] = true
	}
	add := func(mounts []corev1.VolumeMount) error {
		if len(mounts) > volumecontext.MaxVolumesPerPod {
			return invalidHealth()
		}
		for _, m := range mounts {
			if !volumes[m.Name] {
				return invalidHealth()
			}
			c := result[m.Name]
			c.total++
			if m.ReadOnly {
				c.readOnly++
			}
			if c.total > volumecontext.MaxMountsPerVolume {
				return invalidHealth()
			}
			result[m.Name] = c
		}
		return nil
	}
	for _, c := range pod.Spec.Containers {
		if err := add(c.VolumeMounts); err != nil {
			return nil, err
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if err := add(c.VolumeMounts); err != nil {
			return nil, err
		}
	}
	for _, c := range pod.Spec.EphemeralContainers {
		if err := add(c.VolumeMounts); err != nil {
			return nil, err
		}
	}
	return result, nil
}
