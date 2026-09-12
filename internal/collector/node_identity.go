package collector

import (
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"time"
)

// ReconcileNodeIdentities uses the collector's authorised, paged Node inventory.
// A recreated name invalidates both current sources before they can be joined.
// Old history remains bounded and explicitly associated with its previous UID.
func (s *Store) ReconcileNodeIdentities(identities map[string]string, observedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(identities) > s.limits.MaxNodes {
		return ErrStoreCapacity
	}
	if observedAt.Before(s.inventoryUpdatedAt) {
		return ErrSnapshotOutOfOrder
	}
	expected := make(map[string]struct{}, len(identities))
	uids := make(map[string]string, len(identities))
	for name, uid := range identities {
		if name == "" {
			continue
		}
		expected[name], uids[name] = struct{}{}, uid
	}
	for name, snapshot := range s.nodes {
		uid, exists := uids[name]
		if !exists || s.expectedNodeUIDs[name] != "" && s.expectedNodeUIDs[name] != uid {
			s.containerCount -= len(snapshot.containers)
			delete(s.nodes, name)
		}
	}
	for name, value := range s.nodeContext.latest {
		if uids[name] != value.uid {
			delete(s.nodeContext.latest, name)
		}
	}
	s.expectedNodes, s.expectedNodeUIDs = expected, uids
	s.inventoryKnown, s.inventoryUpdatedAt = true, observedAt
	return nil
}

// CurrentNodeIdentities returns a bounded snapshot only while every selected
// identity is known and the inventory remains fresh. It supports retiring
// producer instances for deleted or replaced Nodes when admission is full.
func (s *Store) CurrentNodeIdentities(now time.Time) (map[string]string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.inventoryKnown || now.Before(s.inventoryUpdatedAt) || now.Sub(s.inventoryUpdatedAt) > nodecontext.StaleAfter {
		return nil, false
	}
	identities := make(map[string]string, len(s.expectedNodeUIDs))
	for name, uid := range s.expectedNodeUIDs {
		if uid == "" {
			return nil, false
		}
		identities[name] = uid
	}
	return identities, true
}
