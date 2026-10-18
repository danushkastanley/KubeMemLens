package kube

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

type replicaResolver struct {
	reader       *volumeHealthReader
	authorize    ObjectAuthorizer
	nodeIdentity VolumeNodeIdentity
	calls        *rate.Limiter
	gate         chan struct{}
	now          func() time.Time
}

func NewReplicaResolver(config *rest.Config, authorize ObjectAuthorizer, nodes VolumeNodeIdentity) (replicabaseline.Resolver, error) {
	if config == nil || config.Insecure || !strings.HasPrefix(config.Host, "https://") || authorize == nil || nodes == nil {
		return nil, replicabaseline.ErrInvalid
	}
	reader, err := newVolumeHealthReader(config, VolumeHealthOptions{Namespace: "default", Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	return &replicaResolver{reader: reader, authorize: authorize, nodeIdentity: nodes, calls: rate.NewLimiter(40, 80), gate: make(chan struct{}, 1), now: time.Now}, nil
}

func (r *replicaResolver) Resolve(ctx context.Context, request memoryhistory.Request) (replicabaseline.Selection, error) {
	if request.Validate() != nil || (request.Scope != memoryhistory.Pod && request.Scope != memoryhistory.Workload) {
		return replicabaseline.Selection{}, replicabaseline.ErrInvalid
	}
	select {
	case r.gate <- struct{}{}:
		defer func() { <-r.gate }()
	default:
		return replicabaseline.Selection{}, replicabaseline.ErrBounds
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	q := &markerQuery{objects: map[string]markerObject{}, visible: map[string][]changemarkers.Object{}}
	q.volumeBindingQuery = volumeBindingQuery{healthQuery: healthQuery{reader: r.reader, remaining: 4 << 20, validateJSON: boundedVolumeObject, beforeRequest: r.calls.Wait}, authorize: func(ctx context.Context, access ObjectAccess) error {
		if err := r.calls.Wait(ctx); err != nil {
			return err
		}
		return r.authorize(ctx, access)
	}}
	resource := "workloads"
	if request.Scope == memoryhistory.Pod {
		resource = "pods"
	}
	if err := q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: resource, Subresource: "replicas", Namespace: request.Namespace, Name: request.Name}); err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	requested, root, err := replicaRoot(ctx, q, request)
	if err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	if err := q.authorize(ctx, ObjectAccess{Group: api.MemoryAPIGroup, Resource: "workloads", Subresource: "replicas", Namespace: root.Namespace, Name: root.Name}); err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	details, err := replicaRootDetails(ctx, q, root)
	if err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	rootRequest := memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: root.Namespace, Name: root.Name, WorkloadKind: root.Kind}
	if err := q.preparePods(ctx, rootRequest, root.UID); err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	if len(q.pods) > replicabaseline.MaxPeers {
		return replicabaseline.Selection{}, replicabaseline.ErrBounds
	}
	result := replicabaseline.Selection{Request: request, Requested: requested, Workload: root, Members: []replicabaseline.Member{}}
	names := make([]string, 0, len(q.pods))
	for name := range q.pods {
		names = append(names, name)
	}
	slices.Sort(names)
	revisions := map[string]workloadObject{}
	for _, name := range names {
		pod := q.pods[name]
		member, owners, err := replicaMember(ctx, q, pod)
		if err != nil {
			return replicabaseline.Selection{}, replicaSourceError(err)
		}
		if !slices.Contains(owners, root) {
			continue
		}
		member.Owners = owners
		q.visible[objectKey(member.Object)] = owners
		for i, owner := range owners {
			q.visible[objectKey(owner)] = owners[i+1:]
		}
		if rootChangePending(details) {
			member.Changes = replicabaseline.RecentChange
		}
		if member.Node != "" {
			uid, known := r.nodeIdentity(member.Node, r.now().UTC())
			if known {
				member.NodeUID = uid
			}
		}
		member.Revision, err = replicaRevision(ctx, q, root, pod, owners, revisions)
		if err != nil {
			return replicabaseline.Selection{}, replicaSourceError(err)
		}
		result.Members = append(result.Members, member)
	}
	if request.Scope == memoryhistory.Pod && !slices.ContainsFunc(result.Members, func(m replicabaseline.Member) bool { return m.Object == requested }) {
		return replicabaseline.Selection{}, memoryhistory.ErrChanged
	}
	if err := replicaChangeContext(ctx, q, &result, r.now().UTC()); err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	resourceKind, _ := volumeWorkloadResource(root.Kind)
	current, err := q.workloadObject(ctx, resourceKind, root.Namespace, root.Name)
	if err != nil {
		return replicabaseline.Selection{}, replicaSourceError(err)
	}
	if string(current.UID) != root.UID || current.Generation != details.Generation || current.DeletionTimestamp != nil {
		return replicabaseline.Selection{}, memoryhistory.ErrChanged
	}
	result.Generation, result.ResolvedAt = current.Generation, r.now().UTC()
	return result, ctx.Err()
}

func replicaMember(ctx context.Context, q *markerQuery, pod corev1.Pod) (replicabaseline.Member, []changemarkers.Object, error) {
	for _, group := range []string{api.MemoryAPIGroup, ""} {
		if err := q.authorize(ctx, ObjectAccess{Group: group, Resource: "pods", Namespace: pod.Namespace, Name: pod.Name}); err != nil {
			return replicabaseline.Member{}, nil, err
		}
	}
	member, err := replicaPod(pod)
	if err != nil {
		return member, nil, err
	}
	object, err := replicaPodObject(pod, member.Object)
	if err != nil {
		return member, nil, err
	}
	q.objects[objectKey(member.Object)] = object
	owners, err := q.chain(ctx, object)
	if err != nil {
		return member, nil, err
	}
	return member, owners, nil
}
