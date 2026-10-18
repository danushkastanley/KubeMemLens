package incidentsession

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"k8s.io/utils/clock"
)

type Store struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	clock      clock.WithTicker
	authorizer Authorizer
	limits     Limits
	records    map[string]*record
	newID      func() (string, error)
}

func New(ctx context.Context, limits Limits, authorizer Authorizer) (*Store, error) {
	return newStore(ctx, limits, authorizer, clock.RealClock{})
}

func newStore(parent context.Context, limits Limits, authorizer Authorizer, clk clock.WithTicker) (*Store, error) {
	if parent == nil || parent.Err() != nil || !limits.valid() || authorizer == nil || clk == nil {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Store{ctx: ctx, cancel: cancel, done: make(chan struct{}), clock: clk, authorizer: authorizer,
		limits: limits, records: make(map[string]*record), newID: randomID}
	ticker := clk.NewTicker(time.Second)
	go s.sweep(ticker)
	return s, nil
}

// Shutdown releases retained state and joins the expiry loop. An in-flight
// authorisation cannot reinsert state after cancellation.
func (s *Store) Shutdown() { s.cancel(); <-s.done }

func (s *Store) sweep(ticker clock.Ticker) {
	defer close(s.done)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			s.mu.Lock()
			clear(s.records)
			s.mu.Unlock()
			return
		case <-ticker.C():
			s.mu.Lock()
			s.prune(s.clock.Now())
			s.mu.Unlock()
		}
	}
}

func (s *Store) prune(now time.Time) {
	for id, r := range s.records {
		if !now.Before(r.expires) {
			delete(s.records, id)
		}
	}
}

func (s *Store) authorize(ctx context.Context, p Principal, op Operation, id string) error {
	if ctx == nil || !p.valid() || (op != Create && !digest(id, 16)) {
		return ErrInvalid
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.ctx.Err() != nil {
		return ErrStopped
	}
	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := s.authorizer.Authorize(operation, p, op, id); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.ctx.Err() != nil {
			return ErrStopped
		}
		if errors.Is(err, ErrDenied) {
			return ErrDenied
		}
		return ErrUnavailable
	}
	return s.available(ctx)
}

func (s *Store) available(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.ctx.Err() != nil {
		return ErrStopped
	}
	return nil
}

func (s *Store) find(p Principal, id string) (*record, error) {
	r := s.records[id]
	if r == nil {
		return nil, ErrNotFound
	}
	if !s.clock.Now().Before(r.expires) {
		delete(s.records, id)
		return nil, ErrNotFound
	}
	if r.principal != p {
		return nil, ErrNotFound
	}
	return r, nil
}

func (s *Store) Start(ctx context.Context, principal Principal) (Summary, error) {
	if err := s.authorize(ctx, principal, Create, ""); err != nil {
		return Summary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return Summary{}, err
	}
	now := s.clock.Now()
	s.prune(now)
	namespaceCount := 0
	for _, r := range s.records {
		if r.principal.NamespaceUID == principal.NamespaceUID {
			namespaceCount++
		}
	}
	if len(s.records) >= s.limits.Sessions || namespaceCount >= s.limits.NamespaceSessions {
		return Summary{}, ErrCapacity
	}
	id, err := s.newID()
	if err != nil || !digest(id, 16) || s.records[id] != nil {
		return Summary{}, ErrUnavailable
	}
	p := clonePrincipal(principal)
	r := &record{principal: p, id: id, opened: now, expires: now.Add(s.limits.Retention)}
	in := Input{Kind: Opened, Source: "operator"}
	if err := s.add(ctx, r, in, now); err != nil {
		return Summary{}, err
	}
	s.records[id] = r
	return r.summary(), nil
}

func (s *Store) Append(ctx context.Context, p Principal, id string, input Input) (Summary, error) {
	if err := s.authorize(ctx, p, Append, id); err != nil {
		return Summary{}, err
	}
	if input.Kind == TraceReferenced {
		return Summary{}, ErrInvalid
	}
	if err := input.validate(p); err != nil {
		return Summary{}, err
	}
	return s.mutate(ctx, p, id, input)
}

func (s *Store) Close(ctx context.Context, p Principal, id string) (Summary, error) {
	if err := s.authorize(ctx, p, CloseSession, id); err != nil {
		return Summary{}, err
	}
	return s.mutate(ctx, p, id, Input{Kind: Closed, Source: "operator"})
}

func (s *Store) mutate(ctx context.Context, p Principal, id string, in Input) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return Summary{}, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return Summary{}, err
	}
	if r.closed != nil {
		return Summary{}, ErrClosed
	}
	if err := s.add(ctx, r, in, s.clock.Now()); err != nil {
		if errors.Is(err, ErrCapacity) {
			r.limitReached = true
		}
		return Summary{}, err
	}
	return r.summary(), nil
}

// Reserve a terminal Entry and its byte budget before accepting other entries.
// A rejected append cannot modify the record or extend its original expiry.
func (s *Store) add(ctx context.Context, r *record, in Input, now time.Time) error {
	entryLimit, byteLimit := s.limits.Entries-1, s.limits.SessionBytes-MaxEntryBytes
	if in.Kind == Closed {
		entryLimit++
		byteLimit += MaxEntryBytes
	}
	if len(r.entries) >= entryLimit {
		return ErrCapacity
	}
	e := newEntry(r.principal, in, now, uint64(len(r.entries)+1))
	e.ClockUncertain = entryClockRisk(r, e)
	encoded, err := json.Marshal(e)
	if err != nil || len(encoded) > MaxEntryBytes {
		return ErrInvalid
	}
	next := *r
	next.entries = append(append([]Entry(nil), r.entries...), e)
	if !consistentReferences(next.entries) || !consistentTraceReferences(next.entries) {
		return ErrInvalid
	}
	if in.Kind == Closed {
		at := now.UTC()
		next.closed = &at
	}
	data, err := json.Marshal(fullDocument(&next))
	if err != nil {
		return ErrInvalid
	}
	if len(data) > byteLimit {
		return ErrCapacity
	}
	if err := reserveExportSyntax(&next, data, now); err != nil {
		return err
	}
	if err := s.available(ctx); err != nil {
		return err
	}
	*r = next
	return nil
}

func (s *Store) Delete(ctx context.Context, p Principal, id string) error {
	if err := s.authorize(ctx, p, Delete, id); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return err
	}
	if _, err := s.find(p, id); err != nil {
		return err
	}
	delete(s.records, id)
	return nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
