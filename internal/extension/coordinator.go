package extension

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type AgentClaims struct {
	PodUID       string
	NodeName     string
	NodeUID      string
	CredentialID string
}

type CoordinatorOptions struct {
	Handler    collector.HandlerOptions
	MaxAgents  int
	MaxRetired int
	RetiredTTL time.Duration
	Now        func() time.Time
	Epoch      string
}

type Coordinator struct {
	store *collector.Store
	opts  CoordinatorOptions

	mu      sync.Mutex
	epoch   string
	agents  map[string]agentState
	nodes   map[string]string
	retired map[string]time.Time
}

type agentState struct {
	sequence uint64
	digest   [sha256.Size]byte
	response api.NodeSnapshotResponse
}

type IngestionError struct {
	Status  int
	Code    string
	Message string
	Result  string
}

func (e *IngestionError) Error() string {
	return e.Message
}

func NewCoordinator(store *collector.Store, opts CoordinatorOptions) (*Coordinator, error) {
	if store == nil {
		return nil, fmt.Errorf("ingestion store is required")
	}
	if opts.MaxAgents <= 0 {
		return nil, fmt.Errorf("maximum agent identities must be greater than zero")
	}
	if opts.MaxRetired <= 0 {
		opts.MaxRetired = opts.MaxAgents * 4
	}
	if opts.RetiredTTL <= 0 {
		opts.RetiredTTL = 2 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	epoch := opts.Epoch
	if epoch == "" {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return nil, fmt.Errorf("create ingestion epoch: %w", err)
		}
		epoch = base64.RawURLEncoding.EncodeToString(value[:])
	}
	return &Coordinator{
		store: store, opts: opts, epoch: epoch,
		agents: map[string]agentState{}, nodes: map[string]string{}, retired: map[string]time.Time{},
	}, nil
}

func (c *Coordinator) Epoch(podUID string) api.IngestionEpoch {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneRetired(c.opts.Now())
	return api.IngestionEpoch{
		TypeMeta:      metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IngestionEpoch"},
		ObjectMeta:    metav1.ObjectMeta{Name: "current"},
		Epoch:         c.epoch,
		SchemaVersion: api.CurrentSnapshotSchemaVersion,
		LastSequence:  c.agents[podUID].sequence,
	}
}

func (c *Coordinator) Accept(claims AgentClaims, request api.NodeSnapshotRequest) (api.NodeSnapshotResponse, bool, error) {
	canonical, err := json.Marshal(request)
	if err != nil {
		return api.NodeSnapshotResponse{}, false, reject(400, "invalid_snapshot", "snapshot request cannot be canonicalised", "invalid_snapshot")
	}
	digest := sha256.Sum256(canonical)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneRetired(c.opts.Now())

	if request.Epoch != c.epoch {
		return api.NodeSnapshotResponse{}, false, reject(409, "epoch_mismatch", "ingestion epoch is no longer current", "epoch_mismatch")
	}
	if request.Snapshot.NodeName != claims.NodeName || request.NodeUID != claims.NodeUID {
		return api.NodeSnapshotResponse{}, false, reject(403, "node_claim_mismatch", "snapshot node does not match the authenticated agent", "node_mismatch")
	}
	if request.Sequence == 0 {
		return api.NodeSnapshotResponse{}, false, reject(400, "invalid_sequence", "sequence must be greater than zero", "invalid_sequence")
	}
	if _, retired := c.retired[claims.PodUID]; retired {
		return api.NodeSnapshotResponse{}, false, reject(409, "agent_replaced", "agent instance has been replaced", "replaced_agent")
	}
	state, exists := c.agents[claims.PodUID]
	if exists && request.Sequence == state.sequence {
		if state.digest != digest {
			return api.NodeSnapshotResponse{}, false, reject(409, "sequence_conflict", "sequence was already used for different content", "replay_conflict")
		}
		response := state.response
		response.Duplicate = true
		return response, true, nil
	}
	if exists && request.Sequence < state.sequence {
		return api.NodeSnapshotResponse{}, false, reject(409, "sequence_replayed", "snapshot sequence is older than the last accepted sequence", "replayed")
	}
	owner := c.nodes[claims.NodeName]
	newOwner := owner != "" && owner != claims.PodUID
	if !exists && !newOwner && len(c.agents) >= c.opts.MaxAgents {
		return api.NodeSnapshotResponse{}, false, reject(507, "agent_capacity", "collector agent identity capacity is exhausted", "agent_capacity")
	}
	if newOwner && len(c.retired) >= c.opts.MaxRetired {
		return api.NodeSnapshotResponse{}, false, reject(507, "retired_agent_capacity", "collector retired identity capacity is exhausted", "agent_capacity")
	}
	if request.APIVersion != api.MemoryAPIGroup+"/"+api.MemoryAPIVersion || request.Kind != "NodeSnapshot" {
		return api.NodeSnapshotResponse{}, false, reject(400, "invalid_type", "request apiVersion or kind is invalid", "invalid_snapshot")
	}
	if err := collector.ValidateSnapshot(request.Snapshot, c.opts.Now(), c.opts.Handler); err != nil {
		return api.NodeSnapshotResponse{}, false, reject(400, "invalid_snapshot", err.Error(), "invalid_snapshot")
	}
	count, err := c.store.ReplaceNodeSnapshot(request.Snapshot)
	if errors.Is(err, collector.ErrSnapshotOutOfOrder) {
		return api.NodeSnapshotResponse{}, false, reject(409, "snapshot_out_of_order", err.Error(), "out_of_order")
	}
	if errors.Is(err, collector.ErrStoreCapacity) {
		return api.NodeSnapshotResponse{}, false, reject(507, "store_capacity", err.Error(), "store_capacity")
	}
	if err != nil {
		return api.NodeSnapshotResponse{}, false, reject(500, "store_error", "store snapshot", "store_error")
	}
	response := api.NodeSnapshotResponse{
		TypeMeta:   metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeSnapshotResult"},
		Accepted:   true,
		Containers: count,
	}
	if newOwner {
		delete(c.agents, owner)
		c.retired[owner] = c.opts.Now().Add(c.opts.RetiredTTL)
	}
	c.nodes[claims.NodeName] = claims.PodUID
	c.agents[claims.PodUID] = agentState{sequence: request.Sequence, digest: digest, response: response}
	return response, false, nil
}

func (c *Coordinator) pruneRetired(now time.Time) {
	for podUID, expiresAt := range c.retired {
		if !expiresAt.After(now) {
			delete(c.retired, podUID)
		}
	}
}

func reject(status int, code, message, result string) *IngestionError {
	return &IngestionError{Status: status, Code: code, Message: message, Result: result}
}
