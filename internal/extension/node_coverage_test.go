package extension

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestNodeCoverageReconcilesLinuxInventory(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	_, _ = store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "retired", CapturedAt: now})
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "linux-a", Labels: map[string]string{"kubernetes.io/os": "linux"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "windows-a", Labels: map[string]string{"kubernetes.io/os": "windows"}}},
	)
	probe := newNodeCoverageReadiness(client.CoreV1().Nodes(), store, store.MaxNodes(), "kubernetes.io/os=linux", nil)
	probe.probe(context.Background())
	if err := probe.Check(nil); err != nil {
		t.Fatalf("node inventory readiness: %v", err)
	}
	reliability := store.Reliability(now, time.Minute)
	if reliability.ExpectedNodes != 1 || reliability.MissingNodes != 1 || reliability.State != api.CollectorRebuilding {
		t.Fatalf("inventory reliability = %#v", reliability)
	}
	if nodes := store.ListNodes(now, time.Minute); len(nodes) != 1 || nodes[0].NodeName != "linux-a" || nodes[0].Freshness != api.EvidenceFreshnessMissing {
		t.Fatalf("node inventory = %#v", nodes)
	}
}

func TestNodeCoverageFailureRetainsLastInventoryAndFailsReady(t *testing.T) {
	store := collector.NewStore()
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "linux-a", Labels: map[string]string{"kubernetes.io/os": "linux"}}})
	probe := newNodeCoverageReadiness(client.CoreV1().Nodes(), store, store.MaxNodes(), "kubernetes.io/os=linux", nil)
	probe.probe(context.Background())
	client.PrependReactor("list", "nodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("sensitive transport failure")
	})
	probe.probe(context.Background())
	if !errors.Is(probe.Check(nil), errNodeInventoryUnavailable) {
		t.Fatalf("failed inventory remained ready")
	}
	reliability := store.Reliability(time.Now().UTC(), time.Minute)
	if reliability.ExpectedNodes != 1 || reliability.MissingNodes != 1 {
		t.Fatalf("last known inventory was not retained: %#v", reliability)
	}
}

func TestNodeCoverageMatchesDaemonSetTolerationsAndBoundsInventory(t *testing.T) {
	taint := corev1.Taint{Key: "dedicated", Value: "memory", Effect: corev1.TaintEffectNoSchedule}
	client := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "general", Labels: map[string]string{"kubernetes.io/os": "linux"}}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "memory", Labels: map[string]string{"kubernetes.io/os": "linux"}}, Spec: corev1.NodeSpec{Taints: []corev1.Taint{taint}}},
	)
	store := collector.NewStoreWithHistoryAndLimits(collector.DefaultHistoryOptions(), collector.StoreLimits{MaxNodes: 2, MaxContainers: 10})
	probe := newNodeCoverageReadiness(client.CoreV1().Nodes(), store, store.MaxNodes(), "kubernetes.io/os=linux", nil)
	probe.probe(context.Background())
	if got := store.Reliability(time.Now().UTC(), time.Minute).ExpectedNodes; got != 1 {
		t.Fatalf("untolerated Node count = %d, want 1", got)
	}

	tolerations := []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "memory", Effect: corev1.TaintEffectNoSchedule}}
	probe = newNodeCoverageReadiness(client.CoreV1().Nodes(), store, store.MaxNodes(), "kubernetes.io/os=linux", tolerations)
	probe.probe(context.Background())
	if got := store.Reliability(time.Now().UTC(), time.Minute).ExpectedNodes; got != 2 {
		t.Fatalf("tolerated Node count = %d, want 2", got)
	}

	boundedStore := collector.NewStoreWithHistoryAndLimits(collector.DefaultHistoryOptions(), collector.StoreLimits{MaxNodes: 1, MaxContainers: 10})
	bounded := newNodeCoverageReadiness(client.CoreV1().Nodes(), boundedStore, boundedStore.MaxNodes(), "kubernetes.io/os=linux", tolerations)
	bounded.probe(context.Background())
	if !errors.Is(bounded.Check(nil), errNodeInventoryUnavailable) {
		t.Fatal("over-capacity inventory remained ready")
	}
}

func TestNodeCoveragePagesWithinLimitAndHonoursCancellation(t *testing.T) {
	store := collector.NewStoreWithHistoryAndLimits(collector.DefaultHistoryOptions(), collector.StoreLimits{MaxNodes: 3, MaxContainers: 10})
	lister := &scriptedNodeLister{pages: []*corev1.NodeList{
		{ListMeta: metav1.ListMeta{Continue: "next"}, Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}}},
		{Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-b"}}}},
	}}
	probe := newNodeCoverageReadiness(lister, store, store.MaxNodes(), "pool=memory", nil)
	names, err := probe.listNodeNames(context.Background())
	if err != nil || len(names) != 2 || names[0] != "node-a" || names[1] != "node-b" {
		t.Fatalf("paged names=%v error=%v", names, err)
	}
	if len(lister.options) != 2 || lister.options[0].Limit != nodeCoveragePageSize ||
		lister.options[0].LabelSelector != "pool=memory" || lister.options[1].Continue != "next" {
		t.Fatalf("list options = %#v", lister.options)
	}

	cancelled := newNodeCoverageReadiness(blockingNodeLister{}, store, store.MaxNodes(), "", nil)
	cancelled.timeout = 10 * time.Millisecond
	started := time.Now()
	cancelled.probe(context.Background())
	if time.Since(started) > 250*time.Millisecond || !errors.Is(cancelled.Check(nil), errNodeInventoryUnavailable) {
		t.Fatalf("cancelled inventory did not fail within its bound")
	}
}

type scriptedNodeLister struct {
	pages   []*corev1.NodeList
	options []metav1.ListOptions
}

func (l *scriptedNodeLister) List(_ context.Context, options metav1.ListOptions) (*corev1.NodeList, error) {
	l.options = append(l.options, options)
	if len(l.pages) == 0 {
		return nil, errors.New("unexpected list")
	}
	page := l.pages[0]
	l.pages = l.pages[1:]
	return page, nil
}

type blockingNodeLister struct{}

func (blockingNodeLister) List(ctx context.Context, _ metav1.ListOptions) (*corev1.NodeList, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
