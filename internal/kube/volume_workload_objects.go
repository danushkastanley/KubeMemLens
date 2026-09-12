package kube

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type workloadResource struct{ kind, group, version, resource string }
type workloadObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		Selector json.RawMessage `json:"selector"`
	} `json:"spec"`
}
type workloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []workloadObject `json:"items"`
}

func CanonicalVolumeWorkloadKind(kind string) (string, bool) {
	r, ok := volumeWorkloadResource(kind)
	return r.kind, ok
}
func volumeWorkloadResource(kind string) (workloadResource, bool) {
	for _, r := range []workloadResource{{"Deployment", "apps", "apps/v1", "deployments"}, {"ReplicaSet", "apps", "apps/v1", "replicasets"}, {"StatefulSet", "apps", "apps/v1", "statefulsets"}, {"DaemonSet", "apps", "apps/v1", "daemonsets"}, {"ReplicationController", "", "v1", "replicationcontrollers"}, {"Job", "batch", "batch/v1", "jobs"}, {"CronJob", "batch", "batch/v1", "cronjobs"}} {
		if strings.EqualFold(r.kind, kind) {
			return r, true
		}
	}
	return workloadResource{}, false
}
func (r workloadResource) path(namespace string) string {
	prefix := "/apis/" + r.version
	if r.group == "" {
		prefix = "/api/v1"
	}
	return prefix + "/namespaces/" + namespace + "/" + r.resource
}
func (q *volumeBindingQuery) workloadObject(ctx context.Context, r workloadResource, namespace, name string) (workloadObject, error) {
	var object workloadObject
	err := q.authorisedGet(ctx, VolumeAccess{Group: r.group, Resource: r.resource, Namespace: namespace, Name: name}, r.path(namespace)+"/"+name, &object)
	if err != nil {
		return object, err
	}
	if object.Kind != r.kind || object.APIVersion != r.version || object.Namespace != namespace || object.Name != name || object.UID == "" || len(object.UID) > volumecontext.MaxUIDBytes {
		return workloadObject{}, invalidHealth()
	}
	return object, nil
}

func (q *volumeBindingQuery) workloadPods(ctx context.Context, root workloadObject, r workloadResource) ([]corev1.Pod, error) {
	owners := []workloadObject{root}
	if r.kind == "CronJob" {
		var err error
		owners, err = q.cronJobChildren(ctx, root)
		if err != nil {
			return nil, err
		}
	}
	var pods []corev1.Pod
	seen := map[string]bool{}
	seenUID := map[string]bool{}
	for _, owner := range owners {
		selector, err := workloadSelector(owner)
		if err != nil {
			return nil, err
		}
		var page corev1.PodList
		path := "/api/v1/namespaces/" + root.Namespace + "/pods?limit=33&labelSelector=" + url.QueryEscape(selector)
		if err := q.authorisedGet(ctx, VolumeAccess{Resource: "pods", Namespace: root.Namespace, Verb: "list"}, path, &page); err != nil {
			return nil, err
		}
		if page.Kind != "PodList" || page.APIVersion != "v1" {
			return nil, invalidHealth()
		}
		if page.Continue != "" || len(page.Items) > volumecontext.MaxWorkloadPods {
			return nil, ErrVolumeWorkloadBounds
		}
		for _, pod := range page.Items {
			if pod.Namespace != root.Namespace || !validHealthName(pod.Name) || pod.UID == "" || len(pod.UID) > volumecontext.MaxUIDBytes || seen[pod.Name] || seenUID[string(pod.UID)] {
				return nil, invalidHealth()
			}
			seen[pod.Name] = true
			seenUID[string(pod.UID)] = true
			pods = append(pods, pod)
			if len(pods) > volumecontext.MaxWorkloadPods {
				return nil, ErrVolumeWorkloadBounds
			}
		}
	}
	orderWorkloadPods(pods)
	return pods, nil
}

func workloadSelector(object workloadObject) (string, error) {
	var selector labels.Selector
	if object.Kind == "ReplicationController" {
		var values map[string]string
		if json.Unmarshal(object.Spec.Selector, &values) != nil || len(values) == 0 {
			return "", invalidHealth()
		}
		selector = labels.SelectorFromSet(values)
	} else {
		var value metav1.LabelSelector
		if json.Unmarshal(object.Spec.Selector, &value) != nil {
			return "", invalidHealth()
		}
		var err error
		selector, err = metav1.LabelSelectorAsSelector(&value)
		if err != nil {
			return "", invalidHealth()
		}
	}
	if selector.Empty() || len(selector.String()) > 4096 {
		return "", invalidHealth()
	}
	return selector.String(), nil
}

func (q *volumeBindingQuery) cronJobChildren(ctx context.Context, root workloadObject) ([]workloadObject, error) {
	r, _ := volumeWorkloadResource("Job")
	var result []workloadObject
	continuation := ""
	seen := map[string]bool{}
	count := 0
	for {
		var page workloadList
		path := r.path(root.Namespace) + "?limit=33"
		if continuation != "" {
			path += "&continue=" + url.QueryEscape(continuation)
		}
		if err := q.authorisedGet(ctx, VolumeAccess{Group: r.group, Resource: r.resource, Namespace: root.Namespace, Verb: "list"}, path, &page); err != nil {
			return nil, err
		}
		if page.Kind != "JobList" || page.APIVersion != "batch/v1" {
			return nil, invalidHealth()
		}
		count += len(page.Items)
		if count > 256 || len(page.Items) > 33 {
			return nil, ErrVolumeWorkloadBounds
		}
		for _, job := range page.Items {
			if job.Namespace != root.Namespace || job.UID == "" || len(job.UID) > volumecontext.MaxUIDBytes || !validHealthName(job.Name) {
				return nil, invalidHealth()
			}
			if ownsWorkload(root, controllerReference(job.OwnerReferences)) {
				result = append(result, job)
			}
			if len(result) > volumecontext.MaxWorkloadPods {
				return nil, ErrVolumeWorkloadBounds
			}
		}
		if page.Continue == "" {
			return result, nil
		}
		if len(page.Continue) > 4096 || seen[page.Continue] || len(seen) >= 8 {
			return nil, ErrVolumeWorkloadBounds
		}
		continuation = page.Continue
		seen[continuation] = true
	}
}

func controllerReference(refs []metav1.OwnerReference) *metav1.OwnerReference {
	var result *metav1.OwnerReference
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			if result != nil {
				return nil
			}
			result = &refs[i]
		}
	}
	return result
}
func ownsWorkload(root workloadObject, owner *metav1.OwnerReference) bool {
	return owner != nil && owner.Kind == root.Kind && owner.APIVersion == root.APIVersion && owner.Name == root.Name && owner.UID == root.UID
}
func (q *volumeBindingQuery) workloadMember(ctx context.Context, root workloadObject, refs []metav1.OwnerReference) (bool, error) {
	owner := controllerReference(refs)
	if ownsWorkload(root, owner) {
		return true, nil
	}
	if owner == nil {
		return false, nil
	}
	bridge := ""
	switch root.Kind {
	case "Deployment":
		bridge = "ReplicaSet"
	case "CronJob":
		bridge = "Job"
	default:
		return false, nil
	}
	r, _ := volumeWorkloadResource(bridge)
	if owner.Kind != bridge || owner.APIVersion != r.version {
		return false, nil
	}
	if !validHealthName(owner.Name) || owner.UID == "" || len(owner.UID) > volumecontext.MaxUIDBytes {
		return false, invalidHealth()
	}
	key := r.resource + "/" + owner.Name + "/" + string(owner.UID)
	parent, exists := q.workloadParents[key]
	if !exists {
		var err error
		parent, err = q.workloadObject(ctx, r, root.Namespace, owner.Name)
		if err != nil {
			return false, err
		}
		if q.workloadParents != nil {
			q.workloadParents[key] = parent
		}
	}
	if parent.UID != owner.UID {
		return false, invalidHealth()
	}
	return ownsWorkload(root, controllerReference(parent.OwnerReferences)), nil
}

// This cache lives for one request only. Re-read each distinct parent and
// authorise it again before returning, rather than querying it per replica.
func (q *volumeBindingQuery) recheckWorkloadParents(ctx context.Context) error {
	keys := make([]string, 0, len(q.workloadParents))
	for key := range q.workloadParents {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		prior := q.workloadParents[key]
		r, ok := volumeWorkloadResource(prior.Kind)
		if !ok {
			return invalidHealth()
		}
		current, err := q.workloadObject(ctx, r, prior.Namespace, prior.Name)
		if err != nil {
			return err
		}
		a, b := controllerReference(prior.OwnerReferences), controllerReference(current.OwnerReferences)
		if current.UID != prior.UID || (a == nil) != (b == nil) {
			return invalidHealth()
		}
		if a != nil && (a.UID != b.UID || a.Name != b.Name || a.Kind != b.Kind || a.APIVersion != b.APIVersion) {
			return invalidHealth()
		}
	}
	return nil
}
