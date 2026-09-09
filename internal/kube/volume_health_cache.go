package kube

import corev1 "k8s.io/api/core/v1"

// A live GET with the querying caller's credentials must precede this lookup.
// Exact identity and resource-version equality prevent old cache contents from
// adding data to an authorised but different Pod. The node-wide cache is private.
func (c *PodCache) volumeHealthPod(authorised corev1.Pod) corev1.Pod {
	if !c.Synced() || authorised.Spec.NodeName != c.nodeName || authorised.ResourceVersion == "" {
		return authorised
	}
	object, exists, err := c.informer.GetStore().GetByKey(authorised.Namespace + "/" + authorised.Name)
	if err != nil || !exists {
		return authorised
	}
	pod, ok := object.(*corev1.Pod)
	if !ok || pod.UID != authorised.UID || pod.ResourceVersion != authorised.ResourceVersion || pod.Spec.NodeName != c.nodeName {
		return authorised
	}
	return *pod.DeepCopy()
}
