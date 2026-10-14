package store

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) recordAudit(ctx context.Context, serverID *int, event string, details map[string]any) error {
	return s.recordAuditAs(ctx, nil, serverID, event, details)
}

// RecordAudit records an administrative event with its actor identity.
// It is asynchronous under normal operation so control-plane writes stay fast.
func (s *Store) RecordAudit(ctx context.Context, actor string, serverID *int, event string, details map[string]any) error {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return s.recordAuditAs(ctx, nil, serverID, event, details)
	}
	return s.recordAuditAs(ctx, &actor, serverID, event, details)
}

func (s *Store) recordAuditAs(ctx context.Context, actor *string, serverID *int, event string, details map[string]any) error {
	_ = ctx
	if event == "connect.blocked" {
		if reason, ok := details["reason"].(string); ok && reason != "" {
			s.blockMu.Lock()
			s.blockReasons[reason]++
			s.blockMu.Unlock()
		}
	}
	if s.audits != nil {
		s.audits.enqueue(auditEvent{serverID: serverID, actor: actor, event: event, details: details})
		return nil
	}
	return s.writeAudit(context.Background(), actor, serverID, event, details)
}

func (s *Store) writeAudit(ctx context.Context, actor *string, serverID *int, event string, details map[string]any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("audit persistence panic: %v", recovered)
		}
		if err != nil && s != nil {
			s.auditWriteErrors.Add(1)
		}
	}()
	safeDetails, ok := sanitizeAuditValue(details).(map[string]any)
	if !ok {
		safeDetails = map[string]any{}
	}
	b := s.client.Audit.Create().SetEvent(event).SetDetails(safeDetails)
	if actor != nil {
		b.SetActor(*actor)
	}
	if serverID != nil {
		b.SetServerID(*serverID)
	}
	return b.Exec(ctx)
}

func (s *Store) publishInvalidate(ctx context.Context) {
	s.notifyInvalidation()
	if s.dist != nil {
		if err := s.dist.PublishInvalidate(ctx); err != nil {
			s.distributedErrors.Add(1)
			s.distributedPublishErrors.Add(1)
			s.distributedHealthy.Store(false)
		} else {
			s.distributedHealthy.Store(true)
		}
	}
}

func (s *Store) notifyInvalidation() {
	if s == nil || s.invalidations == nil {
		return
	}
	select {
	case s.invalidations <- struct{}{}:
	default:
	}
}

// Invalidations returns a coalescing notification channel for control-plane
// changes. Gate uses it to refresh backend registrations immediately; the
// periodic sync remains as a recovery path for missed notifications.
func (s *Store) Invalidations() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.invalidations
}
