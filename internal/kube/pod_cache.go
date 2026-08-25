package kube

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// PodCache keeps a node-filtered local view of Pod container status. The
// informer performs one list followed by watches instead of listing every scan.
type PodCache struct {
	informer cache.SharedIndexInformer
	nodeName string
}

func NewPodCache(client kubernetes.Interface, nodeName string) *PodCache {
	selector := fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	informer := v1.NewFilteredPodInformer(
		client,
		metav1.NamespaceAll,
		0,
		cache.Indexers{},
		func(options *metav1.ListOptions) {
			options.FieldSelector = selector
		},
	)
	return &PodCache{informer: informer, nodeName: nodeName}
}

func (c *PodCache) Run(ctx context.Context) {
	c.informer.Run(ctx.Done())
}

func (c *PodCache) WaitForSync(ctx context.Context) bool {
	return cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced)
}

func (c *PodCache) Synced() bool {
	return c.informer.HasSynced()
}

func (c *PodCache) Index() PodIndex {
	objects := c.informer.GetStore().List()
	pods := make([]corev1.Pod, 0, len(objects))
	for _, object := range objects {
		pod, ok := object.(*corev1.Pod)
		if !ok {
			continue
		}
		if pod.Spec.NodeName != c.nodeName {
			continue
		}
		pods = append(pods, *pod)
	}
	return BuildPodIndexFromPods(pods)
}

func (c *PodCache) PodCount() int {
	count := 0
	for _, object := range c.informer.GetStore().List() {
		pod, ok := object.(*corev1.Pod)
		if ok && pod.Spec.NodeName == c.nodeName {
			count++
		}
	}
	return count
}
