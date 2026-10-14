package store

import (
	"context"
	"fmt"
	"strings"

	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	entpredicate "mcproxy/internal/ent/predicate"
	"mcproxy/internal/ent/server"
)

func (s *Store) GetGlobalPolicy(ctx context.Context) (*PolicyDTO, error) {
	return s.cachedGlobalPolicy(ctx)
}

func (s *Store) UpsertGlobalPolicy(ctx context.Context, in PolicyInput) (*PolicyDTO, error) {
	if err := validatePolicyInput(in); err != nil {
		return nil, err
	}
	current, err := s.client.AccessPolicy.Query().Where(accesspolicy.Not(accesspolicy.HasServer())).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if ent.IsNotFound(err) {
		b := s.client.AccessPolicy.Create()
		applyPolicyInputCreate(b, in)
		n, cerr := b.Save(ctx)
		if cerr != nil {
			return nil, cerr
		}
		dto := policyDTO("global", n)
		s.cache.invalidate()
		s.publishInvalidate(ctx)
		return &dto, nil
	}
	b := s.client.AccessPolicy.UpdateOneID(current.ID)
	applyPolicyInputUpdate(b, in)
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := policyDTO("global", n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

func (s *Store) GetServerPolicy(ctx context.Context, serverID int) (*PolicyDTO, error) {
	if _, err := s.client.Server.Get(ctx, serverID); err != nil {
		return nil, err
	}
	p, err := s.client.AccessPolicy.Query().Where(accesspolicy.HasServerWith(server.IDEQ(serverID))).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := policyDTO("server", p)
	return &dto, nil
}

func (s *Store) UpsertServerPolicy(ctx context.Context, serverID int, in PolicyInput) (*PolicyDTO, error) {
	if err := validatePolicyInput(in); err != nil {
		return nil, err
	}
	if _, err := s.client.Server.Get(ctx, serverID); err != nil {
		return nil, err
	}
	current, err := s.client.AccessPolicy.Query().Where(accesspolicy.HasServerWith(server.IDEQ(serverID))).First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if ent.IsNotFound(err) {
		b := s.client.AccessPolicy.Create().SetServerID(serverID)
		applyPolicyInputCreate(b, in)
		n, cerr := b.Save(ctx)
		if cerr != nil {
			return nil, cerr
		}
		dto := policyDTO("server", n)
		s.cache.invalidate()
		s.publishInvalidate(ctx)
		return &dto, nil
	}
	b := s.client.AccessPolicy.UpdateOneID(current.ID)
	applyPolicyInputUpdate(b, in)
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := policyDTO("server", n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

// DeletePolicy removes a scoped override. A global policy is recreated with
// schema defaults on the next read; a server policy falls back to global.
func (s *Store) DeletePolicy(ctx context.Context, serverID *int) error {
	var predicate entpredicate.AccessPolicy
	if serverID == nil {
		predicate = accesspolicy.Not(accesspolicy.HasServer())
	} else {
		if _, err := s.client.Server.Get(ctx, *serverID); err != nil {
			return err
		}
		predicate = accesspolicy.HasServerWith(server.IDEQ(*serverID))
	}
	if _, err := s.client.AccessPolicy.Delete().Where(predicate).Exec(ctx); err != nil {
		return err
	}
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return nil
}

// ResetGeoPolicy disables Geo filtering without changing threshold settings.
func (s *Store) ResetGeoPolicy(ctx context.Context, serverID *int) error {
	var predicate entpredicate.AccessPolicy
	if serverID == nil {
		predicate = accesspolicy.Not(accesspolicy.HasServer())
	} else {
		if _, err := s.client.Server.Get(ctx, *serverID); err != nil {
			return err
		}
		predicate = accesspolicy.HasServerWith(server.IDEQ(*serverID))
	}
	policyNode, err := s.client.AccessPolicy.Query().Where(predicate).First(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.client.AccessPolicy.UpdateOneID(policyNode.ID).SetGeoMode("disabled").ClearGeoList().Exec(ctx); err != nil {
		return err
	}
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return nil
}

func (s *Store) GetGeoPolicy(ctx context.Context, serverID *int) (*PolicyDTO, error) {
	if serverID == nil {
		return s.GetGlobalPolicy(ctx)
	}
	return s.GetServerPolicy(ctx, *serverID)
}

func (s *Store) UpsertGeoPolicy(ctx context.Context, serverID *int, mode string, countries []string) (*PolicyDTO, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch mode {
	case "disabled", "allow", "deny":
	default:
		return nil, fmt.Errorf("invalid geo mode")
	}
	in := PolicyInput{GeoMode: &mode, GeoList: normalizeCountryCodes(countries)}
	if serverID == nil {
		return s.UpsertGlobalPolicy(ctx, in)
	}
	return s.UpsertServerPolicy(ctx, *serverID, in)
}
