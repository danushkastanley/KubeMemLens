package kube

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type NodeContextCache struct {
	client   kubernetes.Interface
	nodeName string
	ttl      time.Duration

	mu        sync.Mutex
	loadedAt  time.Time
	context   api.NodeContext
	lastError error
}

func NewNodeContextCache(client kubernetes.Interface, nodeName string, ttl time.Duration) *NodeContextCache {
	return &NodeContextCache{client: client, nodeName: nodeName, ttl: ttl}
}

func (c *NodeContextCache) Context(ctx context.Context, now time.Time) (api.NodeContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loadedAt.IsZero() && now.Sub(c.loadedAt) < c.ttl {
		return c.context, c.lastError
	}
	c.loadedAt = now
	node, err := c.client.CoreV1().Nodes().Get(ctx, c.nodeName, metav1.GetOptions{})
	if err != nil {
		// Node identity is immutable for the life of the Node object and remains
		// safe to use for authenticated ingestion after a transient GET failure.
		// Other fields describe current control-plane context and must not be
		// presented as fresh when that GET failed.
		nodeUID := c.context.NodeUID
		if apierrors.IsNotFound(err) {
			nodeUID = ""
		}
		c.context = api.NodeContext{NodeUID: nodeUID}
		c.lastError = fmt.Errorf("get node context: %w", err)
		return c.context, c.lastError
	}
	c.context = nodeContext(*node)
	c.lastError = nil
	return c.context, nil
}

func nodeContext(node corev1.Node) api.NodeContext {
	context := api.NodeContext{
		Available:            true,
		NodeUID:              string(node.UID),
		MemoryPressureStatus: "Unknown",
	}
	if allocatable, exists := node.Status.Allocatable[corev1.ResourceMemory]; exists {
		context.MemoryAllocatableKnown = true
		context.MemoryAllocatableBytes = quantityBytes(allocatable)
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeMemoryPressure {
			continue
		}
		context.MemoryPressureStatus = string(condition.Status)
		context.MemoryPressureSince = condition.LastTransitionTime.Time
		break
	}
	return context
}
