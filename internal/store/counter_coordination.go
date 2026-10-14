package store

import (
	"context"
	"fmt"
	"time"
)

// bumpCounter coordinates the distributed counter with the local fallback.
// The bucket implementation itself lives in counter_engine.go so this method
// only owns availability policy and health accounting.
func (s *Store) bumpCounter(ctx context.Context, serverID *int, kind, key string, windowSec int, record bool) (int, error) {
	scope := "global"
	if serverID != nil {
		scope = "server"
	}
	if s.dist != nil {
		countVal, err := s.dist.IncrWindow(ctx, scope, serverID, kind, key, windowSec, record)
		if err == nil {
			s.distributedHealthy.Store(true)
			return countVal, nil
		}
		s.distributedErrors.Add(1)
		s.distributedFallbacks.Add(1)
		s.distributedHealthy.Store(false)
		if s.distributedFailClosed {
			return 0, fmt.Errorf("distributed counter unavailable: %w", err)
		}
	}
	return s.counters.bump(scope, serverID, kind, key, windowSec, record), nil
}

// DistributedLoginReservations reports whether login reservations can be
// shared between Gate replicas. Redis is optional, so callers keep their
// local guard when this is false.
func (s *Store) DistributedLoginReservations() bool {
	return s != nil && s.dist != nil
}

// ReserveConcurrentLogin atomically reserves one modern-client login in Redis
// when distributed coordination is configured. In fail-open mode a Redis
// error is recorded and allowed through so the caller's local guard remains
// effective; fail-closed mode returns the error to deny the attempt.
func (s *Store) ReserveConcurrentLogin(ctx context.Context, ip, member string, limit int) (bool, error) {
	if s == nil || s.dist == nil {
		return true, nil
	}
	allowed, err := s.dist.ReserveLogin(ctx, ip, member, limit, 90*time.Second)
	if err != nil {
		s.distributedErrors.Add(1)
		s.distributedHealthy.Store(false)
		if s.distributedFailClosed {
			return false, fmt.Errorf("distributed login reservation unavailable: %w", err)
		}
		return true, nil
	}
	s.distributedHealthy.Store(true)
	return allowed, nil
}

func (s *Store) ReleaseConcurrentLogin(ctx context.Context, ip, member string) error {
	if s == nil || s.dist == nil {
		return nil
	}
	if err := s.dist.ReleaseLogin(ctx, ip, member); err != nil {
		s.distributedErrors.Add(1)
		s.distributedHealthy.Store(false)
		if s.distributedFailClosed {
			return err
		}
	}
	return nil
}
