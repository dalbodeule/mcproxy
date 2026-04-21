package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // postgres driver for database/sql
	_ "modernc.org/sqlite"             // sqlite driver for database/sql

	"mcproxy/internal/config"
	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	"mcproxy/internal/ent/audit"
	"mcproxy/internal/ent/counter"
	"mcproxy/internal/ent/rule"
	"mcproxy/internal/ent/server"
)

type Store struct {
	db        *sql.DB
	client    *ent.Client
	startedAt time.Time
}

type Stats struct {
	UptimeSeconds int64 `json:"uptime_seconds"`
	Servers       int   `json:"servers"`
	Policies      int   `json:"policies"`
	Rules         int   `json:"rules"`
	Counters      int   `json:"counters"`
	Blocks        int64 `json:"blocks"`
}

type ServerInput struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type ServerDTO struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Upstream  string    `json:"upstream"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PolicyInput struct {
	IPBurst10s   *int     `json:"ip_burst_10s,omitempty"`
	IPBurst60s   *int     `json:"ip_burst_60s,omitempty"`
	NameBurst10s *int     `json:"name_burst_10s,omitempty"`
	NameBurst60s *int     `json:"name_burst_60s,omitempty"`
	DenyFirst    *bool    `json:"deny_first,omitempty"`
	KickMessage  *string  `json:"kick_message,omitempty"`
	GeoMode      *string  `json:"geo_mode,omitempty"`
	GeoList      []string `json:"geo_list,omitempty"`
}

type PolicyDTO struct {
	ID           int       `json:"id"`
	Scope        string    `json:"scope"`
	IPBurst10s   int       `json:"ip_burst_10s"`
	IPBurst60s   int       `json:"ip_burst_60s"`
	NameBurst10s int       `json:"name_burst_10s"`
	NameBurst60s int       `json:"name_burst_60s"`
	DenyFirst    bool      `json:"deny_first"`
	KickMessage  string    `json:"kick_message"`
	GeoMode      string    `json:"geo_mode"`
	GeoList      []string  `json:"geo_list"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RuleInput struct {
	Kind      string     `json:"kind"`
	Target    string     `json:"target"`
	IsCIDR    *bool      `json:"is_cidr,omitempty"`
	Reason    *string    `json:"reason,omitempty"`
	Enabled   *bool      `json:"enabled,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	ServerID  *int       `json:"server_id,omitempty"`
}

type RuleDTO struct {
	ID        int        `json:"id"`
	Scope     string     `json:"scope"`
	ServerID  *int       `json:"server_id,omitempty"`
	Kind      string     `json:"kind"`
	Target    string     `json:"target"`
	IsCIDR    bool       `json:"is_cidr"`
	Reason    *string    `json:"reason,omitempty"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type EvaluateAttemptInput struct {
	ServerID *int   `json:"server_id,omitempty"`
	IP       string `json:"ip"`
	Nickname string `json:"nickname"`
	Country  string `json:"country,omitempty"`
	Record   *bool  `json:"record,omitempty"`
}

type EvaluateAttemptResult struct {
	Allowed bool              `json:"allowed"`
	Reason  string            `json:"reason"`
	Matched map[string]string `json:"matched,omitempty"`
	Counts  map[string]int    `json:"counts,omitempty"`
}

// Open initializes the database connection according to config.
// For sqlite, it ensures the directory for the DB file exists.
func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	var (
		driver = strings.ToLower(cfg.DBDriver)
		dsn    = cfg.DSN
		entDrv string
	)

	switch driver {
	case "sqlite":
		entDrv = "sqlite3"
		// Ensure directory exists if DSN is a file path.
		if dsn != ":memory:" && dsn != "file::memory:?cache=shared" {
			// Handle DSN that may be a URI (e.g., file:path?cache=shared)
			// Try to extract path safely; fallback to raw string.
			p := dsn
			if strings.HasPrefix(dsn, "file:") {
				// file:path or file:/abs/path
				u, err := url.Parse(dsn)
				if err == nil {
					if u.Opaque != "" {
						p = u.Opaque
					} else if u.Path != "" {
						p = u.Path
					}
				}
			}
			if dir := filepath.Dir(p); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, fmt.Errorf("create sqlite dir: %w", err)
				}
			}
		}
	case "postgres":
		entDrv = "postgres"
		if dsn == "" {
			return nil, errors.New("postgres dsn required (MCPROXY_POSTGRES_DSN)")
		}
	default:
		return nil, fmt.Errorf("unsupported db driver: %s", driver)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	// Set conservative defaults; tune later.
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(16)
	db.SetConnMaxLifetime(30 * time.Minute)

	// SQLite-specific tuning: single-connection, WAL, timeout to reduce lock contention.
	if driver == "sqlite" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		// Apply pragmas on the single shared connection.
		_, _ = db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`)
		_, _ = db.ExecContext(ctx, `PRAGMA synchronous=NORMAL;`)
		_, _ = db.ExecContext(ctx, `PRAGMA busy_timeout=5000;`)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(entDrv, db)))
	if err := client.Schema.Create(ctx); err != nil {
		_ = client.Close()
		_ = db.Close()
		return nil, fmt.Errorf("ent schema create: %w", err)
	}

	s := &Store{db: db, client: client, startedAt: time.Now()}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			return err
		}
	}
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Stats returns runtime and persisted entity counts.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	servers, err := s.client.Server.Query().Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	policies, err := s.client.AccessPolicy.Query().Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	rules, err := s.client.Rule.Query().Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	counters, err := s.client.Counter.Query().Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	blocked, err := s.client.Audit.Query().Where(audit.EventEQ("connect.blocked")).Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	up := time.Since(s.startedAt) / time.Second
	return Stats{UptimeSeconds: int64(up), Servers: servers, Policies: policies, Rules: rules, Counters: counters, Blocks: int64(blocked)}, nil
}

func (s *Store) ListServers(ctx context.Context) ([]ServerDTO, error) {
	nodes, err := s.client.Server.Query().Order(ent.Asc(server.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ServerDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, serverDTO(n))
	}
	return out, nil
}

func (s *Store) GetServer(ctx context.Context, id int) (*ServerDTO, error) {
	n, err := s.client.Server.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	return &dto, nil
}

func (s *Store) CreateServer(ctx context.Context, in ServerInput) (*ServerDTO, error) {
	b := s.client.Server.Create().SetName(strings.TrimSpace(in.Name)).SetUpstream(strings.TrimSpace(in.Upstream))
	if in.Enabled != nil {
		b.SetEnabled(*in.Enabled)
	}
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	return &dto, nil
}

func (s *Store) UpdateServer(ctx context.Context, id int, in ServerInput) (*ServerDTO, error) {
	b := s.client.Server.UpdateOneID(id)
	if v := strings.TrimSpace(in.Name); v != "" {
		b.SetName(v)
	}
	if v := strings.TrimSpace(in.Upstream); v != "" {
		b.SetUpstream(v)
	}
	if in.Enabled != nil {
		b.SetEnabled(*in.Enabled)
	}
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := serverDTO(n)
	return &dto, nil
}

func (s *Store) DeleteServer(ctx context.Context, id int) error {
	return s.client.Server.DeleteOneID(id).Exec(ctx)
}

func (s *Store) GetGlobalPolicy(ctx context.Context) (*PolicyDTO, error) {
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

func (s *Store) UpsertGlobalPolicy(ctx context.Context, in PolicyInput) (*PolicyDTO, error) {
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
		return &dto, nil
	}
	b := s.client.AccessPolicy.UpdateOneID(current.ID)
	applyPolicyInputUpdate(b, in)
	n, err := b.Save(ctx)
	if err != nil {
		return nil, err
	}
	dto := policyDTO("global", n)
	return &dto, nil
}

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
		dto := ruleDTO(n)
		out = append(out, dto)
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
	return &dto, nil
}

func (s *Store) DeleteRule(ctx context.Context, id int) error {
	return s.client.Rule.DeleteOneID(id).Exec(ctx)
}

func (s *Store) EvaluateAttempt(ctx context.Context, in EvaluateAttemptInput) (*EvaluateAttemptResult, error) {
	ip := net.ParseIP(strings.TrimSpace(in.IP))
	if ip == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	name := normalizeNickname(in.Nickname)
	record := true
	if in.Record != nil {
		record = *in.Record
	}

	globalPolicy, err := s.GetGlobalPolicy(ctx)
	if err != nil {
		return nil, err
	}
	serverPolicy, err := s.getServerPolicy(ctx, in.ServerID)
	if err != nil {
		return nil, err
	}
	relevantRules, err := s.getActiveRules(ctx, in.ServerID)
	if err != nil {
		return nil, err
	}

	matched := matchRules(relevantRules, ip, name)
	denyFirst := globalPolicy.DenyFirst
	if serverPolicy != nil {
		denyFirst = serverPolicy.DenyFirst
	}

	if denyFirst {
		if id, ok := matched["deny"]; ok {
			_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": "rule.deny", "rule_id": id, "ip": in.IP, "nickname": in.Nickname})
			return &EvaluateAttemptResult{Allowed: false, Reason: "rule.deny", Matched: map[string]string{"rule_id": id}}, nil
		}
		if id, ok := matched["allow"]; ok {
			return &EvaluateAttemptResult{Allowed: true, Reason: "rule.allow", Matched: map[string]string{"rule_id": id}}, nil
		}
	} else {
		if id, ok := matched["allow"]; ok {
			return &EvaluateAttemptResult{Allowed: true, Reason: "rule.allow", Matched: map[string]string{"rule_id": id}}, nil
		}
		if id, ok := matched["deny"]; ok {
			_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": "rule.deny", "rule_id": id, "ip": in.IP, "nickname": in.Nickname})
			return &EvaluateAttemptResult{Allowed: false, Reason: "rule.deny", Matched: map[string]string{"rule_id": id}}, nil
		}
	}

	counts := map[string]int{}
	if in.IP != "" {
		c10, err := s.bumpCounter(ctx, in.ServerID, "ip", in.IP, 10, record)
		if err != nil {
			return nil, err
		}
		c60, err := s.bumpCounter(ctx, in.ServerID, "ip", in.IP, 60, record)
		if err != nil {
			return nil, err
		}
		counts["ip_10s"] = c10
		counts["ip_60s"] = c60
	}
	if name != "" {
		c10, err := s.bumpCounter(ctx, in.ServerID, "name", name, 10, record)
		if err != nil {
			return nil, err
		}
		c60, err := s.bumpCounter(ctx, in.ServerID, "name", name, 60, record)
		if err != nil {
			return nil, err
		}
		counts["name_10s"] = c10
		counts["name_60s"] = c60
	}

	if exceeded, reason := evaluateThresholds(globalPolicy, serverPolicy, counts); exceeded {
		_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": reason, "ip": in.IP, "nickname": in.Nickname, "counts": counts})
		return &EvaluateAttemptResult{Allowed: false, Reason: reason, Counts: counts}, nil
	}

	if blocked, reason := evaluateGeoPolicy(globalPolicy, serverPolicy, strings.ToUpper(strings.TrimSpace(in.Country))); blocked {
		_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": reason, "ip": in.IP, "nickname": in.Nickname, "country": strings.ToUpper(strings.TrimSpace(in.Country))})
		return &EvaluateAttemptResult{Allowed: false, Reason: reason, Counts: counts, Matched: map[string]string{"country": strings.ToUpper(strings.TrimSpace(in.Country))}}, nil
	}

	return &EvaluateAttemptResult{Allowed: true, Reason: "allowed", Counts: counts}, nil
}

func serverDTO(n *ent.Server) ServerDTO {
	return ServerDTO{
		ID:        n.ID,
		Name:      n.Name,
		Upstream:  n.Upstream,
		Enabled:   n.Enabled,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

func policyDTO(scope string, n *ent.AccessPolicy) PolicyDTO {
	return PolicyDTO{
		ID:           n.ID,
		Scope:        scope,
		IPBurst10s:   n.IPBurst10s,
		IPBurst60s:   n.IPBurst60s,
		NameBurst10s: n.NameBurst10s,
		NameBurst60s: n.NameBurst60s,
		DenyFirst:    n.DenyFirst,
		KickMessage:  n.KickMessage,
		GeoMode:      n.GeoMode,
		GeoList:      n.GeoList,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
	}
}

func ruleDTO(n *ent.Rule) RuleDTO {
	var serverID *int
	if n.Edges.Server != nil {
		id := n.Edges.Server.ID
		serverID = &id
	}
	scope := "global"
	if serverID != nil {
		scope = "server"
	}
	return RuleDTO{
		ID:        n.ID,
		Scope:     scope,
		ServerID:  serverID,
		Kind:      n.Kind,
		Target:    n.Target,
		IsCIDR:    n.IsCidr,
		Reason:    n.Reason,
		Enabled:   n.Enabled,
		ExpiresAt: n.ExpiresAt,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

func applyPolicyInputCreate(b *ent.AccessPolicyCreate, in PolicyInput) {
	if in.IPBurst10s != nil {
		b.SetIPBurst10s(*in.IPBurst10s)
	}
	if in.IPBurst60s != nil {
		b.SetIPBurst60s(*in.IPBurst60s)
	}
	if in.NameBurst10s != nil {
		b.SetNameBurst10s(*in.NameBurst10s)
	}
	if in.NameBurst60s != nil {
		b.SetNameBurst60s(*in.NameBurst60s)
	}
	if in.DenyFirst != nil {
		b.SetDenyFirst(*in.DenyFirst)
	}
	if in.KickMessage != nil {
		b.SetKickMessage(*in.KickMessage)
	}
	if in.GeoMode != nil {
		b.SetGeoMode(*in.GeoMode)
	}
	if in.GeoList != nil {
		b.SetGeoList(in.GeoList)
	}
}

func applyPolicyInputUpdate(b *ent.AccessPolicyUpdateOne, in PolicyInput) {
	if in.IPBurst10s != nil {
		b.SetIPBurst10s(*in.IPBurst10s)
	}
	if in.IPBurst60s != nil {
		b.SetIPBurst60s(*in.IPBurst60s)
	}
	if in.NameBurst10s != nil {
		b.SetNameBurst10s(*in.NameBurst10s)
	}
	if in.NameBurst60s != nil {
		b.SetNameBurst60s(*in.NameBurst60s)
	}
	if in.DenyFirst != nil {
		b.SetDenyFirst(*in.DenyFirst)
	}
	if in.KickMessage != nil {
		b.SetKickMessage(*in.KickMessage)
	}
	if in.GeoMode != nil {
		b.SetGeoMode(*in.GeoMode)
	}
	if in.GeoList != nil {
		b.SetGeoList(in.GeoList)
	}
}

func validateRuleInput(in RuleInput) error {
	switch strings.TrimSpace(in.Kind) {
	case "ip-allow", "ip-deny", "name-allow", "name-deny":
	default:
		return fmt.Errorf("invalid kind")
	}
	if strings.TrimSpace(in.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if strings.HasPrefix(in.Kind, "ip-") {
		if in.IsCIDR != nil && *in.IsCIDR {
			if _, _, err := net.ParseCIDR(strings.TrimSpace(in.Target)); err != nil {
				return fmt.Errorf("invalid cidr target")
			}
		} else if net.ParseIP(strings.TrimSpace(in.Target)) == nil {
			return fmt.Errorf("invalid ip target")
		}
	}
	return nil
}

func normalizeNickname(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func (s *Store) getServerPolicy(ctx context.Context, serverID *int) (*PolicyDTO, error) {
	if serverID == nil {
		return nil, nil
	}
	n, err := s.client.AccessPolicy.Query().Where(accesspolicy.HasServerWith(server.IDEQ(*serverID))).First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	dto := policyDTO("server", n)
	return &dto, nil
}

func (s *Store) getActiveRules(ctx context.Context, serverID *int) ([]*ent.Rule, error) {
	now := time.Now()
	global, err := s.client.Rule.Query().Where(rule.Not(rule.HasServer()), rule.EnabledEQ(true)).WithServer().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ent.Rule, 0, len(global)+8)
	for _, n := range global {
		if n.ExpiresAt != nil && n.ExpiresAt.Before(now) {
			continue
		}
		out = append(out, n)
	}
	if serverID == nil {
		return out, nil
	}
	serverRules, err := s.client.Rule.Query().Where(rule.HasServerWith(server.IDEQ(*serverID)), rule.EnabledEQ(true)).WithServer().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range serverRules {
		if n.ExpiresAt != nil && n.ExpiresAt.Before(now) {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func matchRules(rules []*ent.Rule, ip net.IP, nickname string) map[string]string {
	matched := map[string]string{}
	for _, r := range rules {
		if matchRule(r, ip, nickname) {
			switch r.Kind {
			case "ip-allow", "name-allow":
				if _, ok := matched["allow"]; !ok {
					matched["allow"] = fmt.Sprintf("%d", r.ID)
				}
			case "ip-deny", "name-deny":
				if _, ok := matched["deny"]; !ok {
					matched["deny"] = fmt.Sprintf("%d", r.ID)
				}
			}
		}
	}
	return matched
}

func matchRule(r *ent.Rule, ip net.IP, nickname string) bool {
	target := strings.TrimSpace(r.Target)
	switch r.Kind {
	case "ip-allow", "ip-deny":
		if r.IsCidr {
			_, network, err := net.ParseCIDR(target)
			return err == nil && network.Contains(ip)
		}
		parsed := net.ParseIP(target)
		return parsed != nil && parsed.Equal(ip)
	case "name-allow", "name-deny":
		return normalizeNickname(target) == nickname
	default:
		return false
	}
}

func evaluateThresholds(global *PolicyDTO, scoped *PolicyDTO, counts map[string]int) (bool, string) {
	if overThreshold(counts["ip_10s"], chooseMin(global.IPBurst10s, scopedValue(scoped, func(p *PolicyDTO) int { return p.IPBurst10s }))) {
		return true, "threshold.ip_10s"
	}
	if overThreshold(counts["ip_60s"], chooseMin(global.IPBurst60s, scopedValue(scoped, func(p *PolicyDTO) int { return p.IPBurst60s }))) {
		return true, "threshold.ip_60s"
	}
	if overThreshold(counts["name_10s"], chooseMin(global.NameBurst10s, scopedValue(scoped, func(p *PolicyDTO) int { return p.NameBurst10s }))) {
		return true, "threshold.name_10s"
	}
	if overThreshold(counts["name_60s"], chooseMin(global.NameBurst60s, scopedValue(scoped, func(p *PolicyDTO) int { return p.NameBurst60s }))) {
		return true, "threshold.name_60s"
	}
	return false, ""
}

func evaluateGeoPolicy(global *PolicyDTO, scoped *PolicyDTO, country string) (bool, string) {
	mode := global.GeoMode
	list := global.GeoList
	if scoped != nil && scoped.GeoMode != "" && scoped.GeoMode != "disabled" {
		mode = scoped.GeoMode
		list = scoped.GeoList
	}
	if mode == "" || mode == "disabled" {
		return false, ""
	}
	if country == "" {
		if mode == "allow" {
			return true, "geo.country_unknown"
		}
		return false, ""
	}
	matched := false
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), country) {
			matched = true
			break
		}
	}
	switch mode {
	case "allow":
		if !matched {
			return true, "geo.country_not_allowed"
		}
	case "deny":
		if matched {
			return true, "geo.country_denied"
		}
	}
	return false, ""
}

func chooseMin(global, scoped int) int {
	if scoped > 0 && (global == 0 || scoped < global) {
		return scoped
	}
	return global
}

func scopedValue(p *PolicyDTO, fn func(*PolicyDTO) int) int {
	if p == nil {
		return 0
	}
	return fn(p)
}

func overThreshold(current, limit int) bool {
	return limit > 0 && current > limit
}

func (s *Store) bumpCounter(ctx context.Context, serverID *int, kind, key string, windowSec int, record bool) (int, error) {
	q := s.client.Counter.Query().Where(counter.KindEQ(kind), counter.KeyEQ(key), counter.WindowSecEQ(windowSec))
	if serverID != nil {
		q = q.Where(counter.HasServerWith(server.IDEQ(*serverID)))
	} else {
		q = q.Where(counter.Not(counter.HasServer()))
	}
	n, err := q.First(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return 0, err
		}
		if !record {
			return 0, nil
		}
		b := s.client.Counter.Create().SetKind(kind).SetKey(key).SetWindowSec(windowSec).SetCount(1).SetLastSeen(time.Now())
		if serverID != nil {
			b.SetServerID(*serverID)
		}
		created, cerr := b.Save(ctx)
		if cerr != nil {
			return 0, cerr
		}
		return created.Count, nil
	}
	now := time.Now()
	countVal := n.Count
	if now.Sub(n.LastSeen) > time.Duration(windowSec)*time.Second {
		countVal = 0
	}
	if record {
		countVal++
		_, err = s.client.Counter.UpdateOneID(n.ID).SetCount(countVal).SetLastSeen(now).Save(ctx)
		if err != nil {
			return 0, err
		}
	}
	return countVal, nil
}

func (s *Store) recordAudit(ctx context.Context, serverID *int, event string, details map[string]any) error {
	b := s.client.Audit.Create().SetEvent(event).SetDetails(details)
	if serverID != nil {
		b.SetServerID(*serverID)
	}
	return b.Exec(ctx)
}
