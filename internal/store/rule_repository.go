package store

import (
	"context"
	"strings"

	"mcproxy/internal/ent"
	"mcproxy/internal/ent/rule"
	"mcproxy/internal/ent/server"
)

// ListRules returns rules for the requested server scope. Evaluation remains
// in the data-plane path; this file owns only control-plane rule persistence.
func (s *Store) ListRules(ctx context.Context, serverID *int) ([]RuleDTO, error) {
	q := s.client.Rule.Query().Order(ent.Desc(rule.FieldCreatedAt))
	if serverID != nil {
		q = q.Where(rule.HasServerWith(server.IDEQ(*serverID)))
	}
	nodes, err := q.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RuleDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, ruleDTO(n))
	}
	return out, nil
}

func (s *Store) CreateRule(ctx context.Context, in RuleInput) (*RuleDTO, error) {
	if err := validateRuleInput(in); err != nil {
		return nil, err
	}
	b := s.client.Rule.Create().SetKind(strings.TrimSpace(in.Kind)).SetTarget(strings.TrimSpace(in.Target))
	if in.IsCIDR != nil {
		b.SetIsCidr(*in.IsCIDR)
	}
	if in.Reason != nil {
		b.SetReason(strings.TrimSpace(*in.Reason))
	}
	if in.Enabled != nil {
		b.SetEnabled(*in.Enabled)
	}
	if in.ExpiresAt != nil {
		b.SetExpiresAt(*in.ExpiresAt)
	}
	if in.ServerID != nil {
		b.SetServerID(*in.ServerID)
	}
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := ruleDTO(n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

func (s *Store) DeleteRule(ctx context.Context, id int) (*RuleDTO, error) {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) (*RuleDTO, error) {
		_ = tx.Rollback()
		return nil, cause
	}
	n, err := tx.Rule.Query().Where(rule.IDEQ(id)).WithServer().Only(ctx)
	if err != nil {
		return rollback(err)
	}
	dto := ruleDTO(n)
	if err := tx.Rule.DeleteOneID(id).Exec(ctx); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}
