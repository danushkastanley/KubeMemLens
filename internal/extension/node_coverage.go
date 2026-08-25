package extension

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/collector"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/server/healthz"
)

const nodeCoverageInterval = 15 * time.Second
const nodeCoverageTimeout = 15 * time.Second
const nodeCoveragePageSize int64 = 100

var errNodeInventoryUnavailable = errors.New("Kubernetes node inventory is unavailable")

type nodeCoverageReadiness struct {
	client      nodePageLister
	store       *collector.Store
	interval    time.Duration
	timeout     time.Duration
	maxNodes    int
	selector    string
	tolerations []corev1.Toleration

	mu    sync.RWMutex
	ready bool
}

type nodePageLister interface {
	List(context.Context, metav1.ListOptions) (*corev1.NodeList, error)
}

func newNodeCoverageReadiness(client nodePageLister, store *collector.Store, maxNodes int, selector string, tolerations []corev1.Toleration) *nodeCoverageReadiness {
	return &nodeCoverageReadiness{
		client: client, store: store, interval: nodeCoverageInterval, timeout: nodeCoverageTimeout,
		maxNodes: maxNodes, selector: selector, tolerations: daemonSetTolerations(tolerations),
	}
}

func (p *nodeCoverageReadiness) Name() string {
	return "node-inventory"
}

func (p *nodeCoverageReadiness) Check(*http.Request) error {
	p.mu.RLock()
	ready := p.ready
	p.mu.RUnlock()
	if !ready {
		return errNodeInventoryUnavailable
	}
	return nil
}

func (p *nodeCoverageReadiness) Run(ctx context.Context) {
	p.probe(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.probe(ctx)
		}
	}
}

func (p *nodeCoverageReadiness) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	names, err := p.listNodeNames(probeCtx)
	ready := err == nil && p.store.ReconcileExpectedNodes(names, time.Now().UTC()) == nil
	p.mu.Lock()
	p.ready = ready
	p.mu.Unlock()
}

func (p *nodeCoverageReadiness) listNodeNames(ctx context.Context) ([]string, error) {
	names := make([]string, 0, min(p.maxNodes, int(nodeCoveragePageSize)))
	continuation := ""
	seen := map[string]struct{}{}
	scanned := 0
	for {
		page, err := p.client.List(ctx, metav1.ListOptions{
			LabelSelector: p.selector, Limit: nodeCoveragePageSize, Continue: continuation,
		})
		if err != nil || page == nil {
			return nil, errNodeInventoryUnavailable
		}
		scanned += len(page.Items)
		if scanned > p.maxNodes {
			return nil, collector.ErrStoreCapacity
		}
		for _, node := range page.Items {
			if nodeTolerated(node.Spec.Taints, p.tolerations) {
				names = append(names, node.Name)
			}
		}
		continuation = page.Continue
		if continuation == "" {
			sort.Strings(names)
			return names, nil
		}
		if scanned >= p.maxNodes {
			return nil, collector.ErrStoreCapacity
		}
		if _, duplicate := seen[continuation]; duplicate {
			return nil, errNodeInventoryUnavailable
		}
		seen[continuation] = struct{}{}
	}
}

func daemonSetTolerations(configured []corev1.Toleration) []corev1.Toleration {
	result := append([]corev1.Toleration(nil), configured...)
	for _, item := range []struct {
		key    string
		effect corev1.TaintEffect
	}{
		{"node.kubernetes.io/not-ready", corev1.TaintEffectNoExecute},
		{"node.kubernetes.io/unreachable", corev1.TaintEffectNoExecute},
		{"node.kubernetes.io/disk-pressure", corev1.TaintEffectNoSchedule},
		{"node.kubernetes.io/memory-pressure", corev1.TaintEffectNoSchedule},
		{"node.kubernetes.io/pid-pressure", corev1.TaintEffectNoSchedule},
		{"node.kubernetes.io/unschedulable", corev1.TaintEffectNoSchedule},
	} {
		result = append(result, corev1.Toleration{Key: item.key, Operator: corev1.TolerationOpExists, Effect: item.effect})
	}
	return result
}

func nodeTolerated(taints []corev1.Taint, tolerations []corev1.Toleration) bool {
	for _, taint := range taints {
		if taint.Effect == corev1.TaintEffectPreferNoSchedule {
			continue
		}
		tolerated := false
		for _, toleration := range tolerations {
			effectMatches := toleration.Effect == "" || toleration.Effect == taint.Effect
			keyMatches := toleration.Key == taint.Key || toleration.Key == "" && toleration.Operator == corev1.TolerationOpExists
			valueMatches := toleration.Operator == corev1.TolerationOpExists || toleration.Value == taint.Value
			if effectMatches && keyMatches && valueMatches {
				tolerated = true
				break
			}
		}
		if !tolerated {
			return false
		}
	}
	return true
}

var _ healthz.HealthChecker = (*nodeCoverageReadiness)(nil)
