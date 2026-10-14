package store

import (
	"context"
	"strings"

	"mcproxy/internal/ent"
	"mcproxy/internal/ent/audit"
	entpredicate "mcproxy/internal/ent/predicate"
	"mcproxy/internal/ent/server"
)

const maxAuditOffset = 1_000_000

// ListAudits is the control-plane audit read path. The server filter is kept
// here so scoped administration does not share query construction with the
// asynchronous writer.
func (s *Store) ListAudits(ctx context.Context, limit, offset int, event, actor string, serverIDs []int) ([]AuditDTO, error) {
	// 101 is reserved for the API's look-ahead query; public responses still
	// return at most the requested 100 records.
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxAuditOffset {
		offset = maxAuditOffset
	}
	q := s.client.Audit.Query().Order(ent.Desc(audit.FieldCreatedAt), ent.Desc(audit.FieldID)).Limit(limit).Offset(offset).WithServer()
	if event = strings.TrimSpace(event); event != "" {
		q = q.Where(audit.EventEQ(event))
	}
	if actor = strings.TrimSpace(actor); actor != "" {
		q = q.Where(audit.ActorContainsFold(actor))
	}
	if len(serverIDs) > 0 {
		predicates := make([]entpredicate.Audit, 0, len(serverIDs))
		for _, serverID := range serverIDs {
			if serverID > 0 {
				predicates = append(predicates, audit.HasServerWith(server.IDEQ(serverID)))
			}
		}
		if len(predicates) > 0 {
			q = q.Where(audit.Or(predicates...))
		}
	}
	nodes, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AuditDTO, 0, len(nodes))
	for _, n := range nodes {
		var serverID *int
		if n.Edges.Server != nil {
			id := n.Edges.Server.ID
			serverID = &id
		}
		out = append(out, AuditDTO{ID: n.ID, Event: n.Event, Actor: n.Actor, ServerID: serverID, Details: n.Details, CreatedAt: n.CreatedAt})
	}
	return out, nil
}
