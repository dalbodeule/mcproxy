package store

import (
	"context"
	"sync"
	"time"

	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	"mcproxy/internal/ent/rule"
)

type snapshotCache struct {
	mu            sync.RWMutex
	globalPolicy  *PolicyDTO
	serverPolicy  map[int]*PolicyDTO
	globalRules   []*ent.Rule
	serverRules   map[int][]*ent.Rule
	loadedAt      time.Time
	refreshWindow time.Duration
}

func newSnapshotCache() *snapshotCache {
	return &snapshotCache{
		serverPolicy:  make(map[int]*PolicyDTO),
		serverRules:   make(map[int][]*ent.Rule),
		refreshWindow: 5 * time.Second,
	}
}

func (c *snapshotCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalPolicy = nil
	c.serverPolicy = make(map[int]*PolicyDTO)
	c.globalRules = nil
	c.serverRules = make(map[int][]*ent.Rule)
	c.loadedAt = time.Time{}
}

func (c *snapshotCache) stale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loadedAt.IsZero() || time.Since(c.loadedAt) > c.refreshWindow
}

func (s *Store) refreshSnapshots(ctx context.Context) error {
	globalPolicy, err := s.loadGlobalPolicy(ctx)
	if err != nil {
		return err
	}
	policies, err := s.client.AccessPolicy.Query().Where(accesspolicy.HasServer()).All(ctx)
	if err != nil {
		return err
	}
	rules, err := s.client.Rule.Query().Where(rule.EnabledEQ(true)).WithServer().All(ctx)
	if err != nil {
		return err
	}
	serverPolicy := make(map[int]*PolicyDTO)
	for _, p := range policies {
		srv, qerr := s.client.AccessPolicy.QueryServer(p).Only(ctx)
		if qerr != nil {
			continue
		}
		dto := policyDTO("server", p)
		serverPolicy[srv.ID] = &dto
	}
	globalRules := make([]*ent.Rule, 0)
	serverRules := make(map[int][]*ent.Rule)
	now := time.Now()
	for _, r := range rules {
		if r.ExpiresAt != nil && r.ExpiresAt.Before(now) {
			continue
		}
		if r.Edges.Server == nil {
			globalRules = append(globalRules, r)
			continue
		}
		serverRules[r.Edges.Server.ID] = append(serverRules[r.Edges.Server.ID], r)
	}
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.globalPolicy = globalPolicy
	s.cache.serverPolicy = serverPolicy
	s.cache.globalRules = globalRules
	s.cache.serverRules = serverRules
	s.cache.loadedAt = time.Now()
	return nil
}

func (s *Store) cachedGlobalPolicy(ctx context.Context) (*PolicyDTO, error) {
	if s.cache.stale() {
		if err := s.refreshSnapshots(ctx); err != nil {
			return nil, err
		}
	}
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	if s.cache.globalPolicy == nil {
		return nil, nil
	}
	cp := *s.cache.globalPolicy
	return &cp, nil
}

func (s *Store) cachedServerPolicy(ctx context.Context, serverID *int) (*PolicyDTO, error) {
	if serverID == nil {
		return nil, nil
	}
	if s.cache.stale() {
		if err := s.refreshSnapshots(ctx); err != nil {
			return nil, err
		}
	}
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	p := s.cache.serverPolicy[*serverID]
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *Store) cachedRules(ctx context.Context, serverID *int) ([]*ent.Rule, error) {
	if s.cache.stale() {
		if err := s.refreshSnapshots(ctx); err != nil {
			return nil, err
		}
	}
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	out := make([]*ent.Rule, 0, len(s.cache.globalRules)+8)
	out = append(out, s.cache.globalRules...)
	if serverID != nil {
		out = append(out, s.cache.serverRules[*serverID]...)
	}
	return out, nil
}

func (s *Store) loadGlobalPolicy(ctx context.Context) (*PolicyDTO, error) {
	n, err := s.client.AccessPolicy.Query().Where(accesspolicy.Not(accesspolicy.HasServer())).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			created, cerr := s.client.AccessPolicy.Create().Save(ctx)
			if cerr != nil {
				return nil, cerr
			}
			dto := policyDTO("global", created)
			return &dto, nil
		}
		return nil, err
	}
	dto := policyDTO("global", n)
	return &dto, nil
}

func (s *Store) preloadServerPolicy(ctx context.Context, id int) (*ent.Server, error) {
	return s.client.Server.Get(ctx, id)
}
