package store

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	entmodel "mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	"mcproxy/internal/ent/audit"
	"mcproxy/internal/ent/counter"
	"mcproxy/internal/ent/rule"
	"mcproxy/internal/ent/server"
)

func (s *Store) ListServers(ctx context.Context) ([]ServerDTO, error) {
	nodes, err := s.client.Server.Query().Order(entmodel.Asc(server.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ServerDTO, 0, len(nodes))
	for i, n := range nodes {
		if !isValidBungeeGuardToken(n.BungeeguardAllowedToken) {
			if nodes[i], err = s.ensureBungeeGuardToken(ctx, n); err != nil {
				return nil, err
			}
			n = nodes[i]
		}
		out = append(out, serverDTO(n))
	}
	return out, nil
}

func (s *Store) GetServer(ctx context.Context, id int) (*ServerDTO, error) {
	n, err := s.client.Server.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isValidBungeeGuardToken(n.BungeeguardAllowedToken) {
		if n, err = s.ensureBungeeGuardToken(ctx, n); err != nil {
			return nil, err
		}
	}
	dto := serverDTO(n)
	return &dto, nil
}

func (s *Store) CreateServer(ctx context.Context, in ServerInput) (*ServerDTO, error) {
	if err := validateServerInput(in, true); err != nil {
		return nil, err
	}
	token, err := newBungeeGuardToken()
	if err != nil {
		return nil, fmt.Errorf("generate bungeeguard token: %w", err)
	}
	b := s.client.Server.Create().SetName(strings.TrimSpace(in.Name)).SetUpstream(strings.TrimSpace(in.Upstream)).SetForwardingMode(normalizeForwardingMode(in.ForwardingMode)).SetBungeeguardAllowedToken(token)
	if in.PublicHost != nil {
		b.SetPublicHost(strings.ToLower(strings.TrimSpace(*in.PublicHost)))
	}
	if in.PublicAddr != nil {
		b.SetPublicAddr(strings.TrimSpace(*in.PublicAddr))
	}
	if in.PingMOTD != nil {
		b.SetPingMotd(strings.TrimSpace(*in.PingMOTD))
	}
	if in.Priority != nil {
		b.SetPriority(*in.Priority)
	}
	if in.Enabled != nil {
		b.SetEnabled(*in.Enabled)
	}
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

func (s *Store) UpdateServer(ctx context.Context, id int, in ServerInput) (*ServerDTO, error) {
	if err := validateServerInput(in, false); err != nil {
		return nil, err
	}
	b := s.client.Server.UpdateOneID(id)
	if v := strings.TrimSpace(in.Name); v != "" {
		b.SetName(v)
	}
	if v := strings.TrimSpace(in.Upstream); v != "" {
		b.SetUpstream(v)
	}
	if in.ForwardingMode != nil {
		b.SetForwardingMode(normalizeForwardingMode(in.ForwardingMode))
	}
	if in.PublicHost != nil {
		b.SetPublicHost(strings.ToLower(strings.TrimSpace(*in.PublicHost)))
	}
	if in.PublicAddr != nil {
		b.SetPublicAddr(strings.TrimSpace(*in.PublicAddr))
	}
	if in.PingMOTD != nil {
		b.SetPingMotd(strings.TrimSpace(*in.PingMOTD))
	}
	if in.Priority != nil {
		b.SetPriority(*in.Priority)
	}
	if in.Enabled != nil {
		b.SetEnabled(*in.Enabled)
	}
	current, err := s.client.Server.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isValidBungeeGuardToken(current.BungeeguardAllowedToken) {
		token, tokenErr := newBungeeGuardToken()
		if tokenErr != nil {
			return nil, fmt.Errorf("generate bungeeguard token: %w", tokenErr)
		}
		b.SetBungeeguardAllowedToken(token)
	}
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

// RotateBungeeGuardToken replaces the per-backend token. The backend plugin's
// allowed-tokens must be updated immediately after rotation.
func (s *Store) RotateBungeeGuardToken(ctx context.Context, id int) (*ServerDTO, error) {
	token, err := newBungeeGuardToken()
	if err != nil {
		return nil, fmt.Errorf("generate bungeeguard token: %w", err)
	}
	n, err := s.client.Server.UpdateOneID(id).SetBungeeguardAllowedToken(token).Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return &dto, nil
}

func (s *Store) DeleteServer(ctx context.Context, id int) error {
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if _, err := tx.AccessPolicy.Delete().Where(accesspolicy.HasServerWith(server.IDEQ(id))).Exec(ctx); err != nil {
		return rollback(err)
	}
	if _, err := tx.Rule.Delete().Where(rule.HasServerWith(server.IDEQ(id))).Exec(ctx); err != nil {
		return rollback(err)
	}
	if _, err := tx.Counter.Delete().Where(counter.HasServerWith(server.IDEQ(id))).Exec(ctx); err != nil {
		return rollback(err)
	}
	if err := tx.Audit.Update().Where(audit.HasServerWith(server.IDEQ(id))).ClearServer().Exec(ctx); err != nil {
		return rollback(err)
	}
	if err := tx.Server.DeleteOneID(id).Exec(ctx); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.cache.invalidate()
	s.publishInvalidate(ctx)
	return nil
}

func serverDTO(n *entmodel.Server) ServerDTO {
	publicHost := ""
	if n.PublicHost != nil && strings.TrimSpace(*n.PublicHost) != "" {
		publicHost = strings.TrimSpace(*n.PublicHost)
	}
	publicAddr := ""
	if n.PublicAddr != nil {
		publicAddr = strings.TrimSpace(*n.PublicAddr)
	}
	return ServerDTO{ID: n.ID, Name: n.Name, PublicHost: publicHost, PublicAddr: publicAddr, Upstream: n.Upstream, ForwardingMode: normalizeForwardingModeValue(n.ForwardingMode), PingMOTD: strings.TrimSpace(n.PingMotd), Priority: n.Priority, Enabled: n.Enabled, BungeeGuardAllowedToken: n.BungeeguardAllowedToken, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt}
}

func newBungeeGuardToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 32)
	for i, value := range buf {
		out[i*2] = hex[value>>4]
		out[i*2+1] = hex[value&0x0f]
	}
	return string(out), nil
}

func isValidBungeeGuardToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, ch := range token {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func (s *Store) ensureBungeeGuardToken(ctx context.Context, n *entmodel.Server) (*entmodel.Server, error) {
	token, err := newBungeeGuardToken()
	if err != nil {
		return nil, fmt.Errorf("generate bungeeguard token: %w", err)
	}
	updated, err := s.client.Server.UpdateOneID(n.ID).
		Where(server.BungeeguardAllowedTokenEQ(n.BungeeguardAllowedToken)).
		SetBungeeguardAllowedToken(token).
		Save(ctx)
	if err == nil {
		return updated, nil
	}
	if entmodel.IsNotFound(err) {
		return s.client.Server.Get(ctx, n.ID)
	}
	return nil, err
}
