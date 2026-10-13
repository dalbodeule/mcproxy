package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // postgres driver for database/sql
	_ "modernc.org/sqlite"             // sqlite driver for database/sql

	"mcproxy/internal/config"
	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	"mcproxy/internal/ent/audit"
	"mcproxy/internal/ent/counter"
	entpredicate "mcproxy/internal/ent/predicate"
	"mcproxy/internal/ent/rule"
	"mcproxy/internal/ent/server"
)

type Store struct {
	db            *sql.DB
	client        *ent.Client
	startedAt     time.Time
	cache         *snapshotCache
	counters      *bucketCounter
	audits        *auditWriter
	dist          *distributedCoordinator
	invalidations chan struct{}
	blockMu       sync.RWMutex
	blockReasons  map[string]int64
}

type Stats struct {
	UptimeSeconds  int64            `json:"uptime_seconds"`
	Servers        int              `json:"servers"`
	EnabledServers int              `json:"enabled_servers"`
	Policies       int              `json:"policies"`
	Rules          int              `json:"rules"`
	Counters       int              `json:"counters"`
	Blocks         int64            `json:"blocks"`
	BlockReasons   map[string]int64 `json:"block_reasons,omitempty"`
}

type AuditDTO struct {
	ID        int            `json:"id"`
	Event     string         `json:"event"`
	Actor     *string        `json:"actor,omitempty"`
	ServerID  *int           `json:"server_id,omitempty"`
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"created_at"`
}

type ServerInput struct {
	Name           string  `json:"name"`
	PublicHost     *string `json:"public_host,omitempty"`
	PublicAddr     *string `json:"public_addr,omitempty"`
	Upstream       string  `json:"upstream"`
	ForwardingMode *string `json:"forwarding_mode,omitempty"`
	PingMOTD       *string `json:"ping_motd,omitempty"`
	Priority       *int    `json:"priority,omitempty"`
	Enabled        *bool   `json:"enabled,omitempty"`
}

type ServerDTO struct {
	ID                      int       `json:"id"`
	Name                    string    `json:"name"`
	PublicHost              string    `json:"public_host,omitempty"`
	PublicAddr              string    `json:"public_addr,omitempty"`
	Upstream                string    `json:"upstream"`
	ForwardingMode          string    `json:"forwarding_mode"`
	PingMOTD                string    `json:"ping_motd,omitempty"`
	Priority                int       `json:"priority"`
	Enabled                 bool      `json:"enabled"`
	BungeeGuardAllowedToken string    `json:"bungeeguard_allowed_token"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
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
		driver    = strings.ToLower(cfg.DBDriver)
		sqlDriver string
		dsn       = cfg.DSN
		entDrv    string
	)

	switch driver {
	case "sqlite":
		sqlDriver = "sqlite"
		entDrv = "sqlite3"
		// Ensure directory exists if DSN is a file path.
		if dsn != ":memory:" && dsn != "file::memory:?cache=shared" {
			// Handle DSN that may be a URI (e.g., file:path?cache=shared)
			// or plain path with query parameters (e.g. data/mcproxy.db?_fk=1).
			// Try to extract only the filesystem path part safely.
			p := dsn
			if i := strings.IndexByte(p, '?'); i >= 0 {
				p = p[:i]
			}
			if strings.HasPrefix(p, "file:") {
				// file:path or file:/abs/path
				u, err := url.Parse(p)
				if err == nil {
					if u.Opaque != "" {
						p = u.Opaque
					} else if u.Path != "" {
						p = u.Path
					}
				}
			}
			if decoded, err := url.PathUnescape(p); err == nil && decoded != "" {
				p = decoded
			}
			if dir := filepath.Dir(p); dir != "." && dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return nil, fmt.Errorf("create sqlite dir: %w", err)
				}
			}
		}
	case "postgres":
		// pgx/stdlib registers the database/sql driver as "pgx" while
		// ent still needs the dialect name "postgres".
		sqlDriver = "pgx"
		entDrv = "postgres"
		if dsn == "" {
			return nil, errors.New("postgres dsn required (MCPROXY_POSTGRES_DSN)")
		}
	default:
		return nil, fmt.Errorf("unsupported db driver: %s", driver)
	}

	db, err := sql.Open(sqlDriver, dsn)
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
		_, _ = db.ExecContext(ctx, `PRAGMA foreign_keys=ON;`)
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

	s := &Store{db: db, client: client, startedAt: time.Now(), cache: newSnapshotCache(), counters: newBucketCounter(), blockReasons: make(map[string]int64), invalidations: make(chan struct{}, 1)}
	dist, derr := newDistributedCoordinator(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisTLS, cfg.RedisChannel, func() {
		s.cache.invalidate()
		s.notifyInvalidation()
	})
	if derr != nil {
		_ = client.Close()
		_ = db.Close()
		return nil, fmt.Errorf("distributed coordinator: %w", derr)
	}
	s.dist = dist
	s.audits = newAuditWriter(s)
	_ = s.refreshSnapshots(ctx)
	return s, nil
}

// Migrate applies the current Ent schema without starting the Redis
// coordinator or Gate runtime. Normal application startup also calls the same
// schema creation path, so this command is safe to run repeatedly.
func Migrate(ctx context.Context, cfg config.Config) error {
	cfg.RedisAddr = ""
	st, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	return st.Close()
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.audits != nil {
		s.audits.stop()
	}
	if s.dist != nil {
		if err := s.dist.Close(); err != nil {
			return err
		}
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
	enabledServers, err := s.client.Server.Query().Where(server.EnabledEQ(true)).Count(ctx)
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
	counters := s.counters.size()
	if s.dist != nil {
		distributedCounters, derr := s.dist.CountCounters(ctx)
		if derr != nil {
			return Stats{}, derr
		}
		counters = distributedCounters
	}
	blocked, err := s.client.Audit.Query().Where(audit.EventEQ("connect.blocked")).Count(ctx)
	if err != nil {
		return Stats{}, err
	}
	up := time.Since(s.startedAt) / time.Second
	s.blockMu.RLock()
	blockReasons := make(map[string]int64, len(s.blockReasons))
	for reason, count := range s.blockReasons {
		blockReasons[reason] = count
	}
	s.blockMu.RUnlock()
	return Stats{UptimeSeconds: int64(up), Servers: servers, EnabledServers: enabledServers, Policies: policies, Rules: rules, Counters: counters, Blocks: int64(blocked), BlockReasons: blockReasons}, nil
}

// Health verifies that the backing database is reachable. It is intentionally
// separate from Stats so readiness checks do not enumerate application tables.
func (s *Store) Health(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}
	return s.db.PingContext(ctx)
}

func (s *Store) ListServers(ctx context.Context) ([]ServerDTO, error) {
	nodes, err := s.client.Server.Query().Order(ent.Asc(server.FieldID)).All(ctx)
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
	// Preserve audit history while removing the foreign-key relationship.
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
	in := PolicyInput{
		GeoMode: &mode,
		GeoList: normalizeCountryCodes(countries),
	}
	if serverID == nil {
		return s.UpsertGlobalPolicy(ctx, in)
	}
	return s.UpsertServerPolicy(ctx, *serverID, in)
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

func (s *Store) ListAudits(ctx context.Context, limit, offset int, event string) ([]AuditDTO, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q := s.client.Audit.Query().Order(ent.Desc(audit.FieldCreatedAt), ent.Desc(audit.FieldID)).Limit(limit).Offset(offset).WithServer()
	if event = strings.TrimSpace(event); event != "" {
		q = q.Where(audit.EventEQ(event))
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

func (s *Store) DeleteRule(ctx context.Context, id int) error {
	err := s.client.Rule.DeleteOneID(id).Exec(ctx)
	if err == nil {
		s.cache.invalidate()
		s.publishInvalidate(ctx)
	}
	return err
}

func (s *Store) EvaluateAttempt(ctx context.Context, in EvaluateAttemptInput) (*EvaluateAttemptResult, error) {
	rawIP := strings.TrimSpace(in.IP)
	if len(rawIP) > 128 {
		return nil, fmt.Errorf("ip is too long")
	}
	if len([]rune(strings.TrimSpace(in.Nickname))) > 128 {
		return nil, fmt.Errorf("nickname is too long")
	}
	if len([]rune(strings.TrimSpace(in.Country))) > 16 {
		return nil, fmt.Errorf("country is too long")
	}
	ip := net.ParseIP(rawIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	name := normalizeNickname(in.Nickname)
	record := true
	if in.Record != nil {
		record = *in.Record
	}

	globalPolicy, err := s.cachedGlobalPolicy(ctx)
	if err != nil {
		return nil, err
	}
	serverPolicy, err := s.cachedServerPolicy(ctx, in.ServerID)
	if err != nil {
		return nil, err
	}
	relevantRules, err := s.cachedRules(ctx, in.ServerID)
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

func (s *Store) ensureBungeeGuardToken(ctx context.Context, n *ent.Server) (*ent.Server, error) {
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
	// Another request may have initialized the token first. Return the
	// winner instead of overwriting it or failing a normal server listing.
	if ent.IsNotFound(err) {
		return s.client.Server.Get(ctx, n.ID)
	}
	return nil, err
}

func validateServerInput(in ServerInput, create bool) error {
	if create && strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if create && strings.TrimSpace(in.Upstream) == "" {
		return fmt.Errorf("upstream is required")
	}
	if strings.TrimSpace(in.Name) != "" && len([]rune(strings.TrimSpace(in.Name))) > 128 {
		return fmt.Errorf("name is too long")
	}
	if strings.TrimSpace(in.Upstream) != "" {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(in.Upstream)); err != nil {
			return fmt.Errorf("upstream must be host:port")
		}
	}
	if in.Priority != nil && (*in.Priority < 0 || *in.Priority > 1_000_000) {
		return fmt.Errorf("priority must be between 0 and 1000000")
	}
	if in.PublicHost != nil && strings.TrimSpace(*in.PublicHost) != "" {
		host := strings.TrimSpace(*in.PublicHost)
		if strings.ContainsAny(host, "/?#@") || net.ParseIP(host) != nil {
			return fmt.Errorf("invalid public_host")
		}
	}
	if in.PublicAddr != nil && strings.TrimSpace(*in.PublicAddr) != "" {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(*in.PublicAddr)); err != nil {
			return fmt.Errorf("public_addr must be host:port")
		}
	}
	if in.PingMOTD != nil && len([]rune(strings.TrimSpace(*in.PingMOTD))) > 256 {
		return fmt.Errorf("ping_motd is too long")
	}
	if in.ForwardingMode != nil && !isValidForwardingMode(*in.ForwardingMode) {
		return fmt.Errorf("forwarding_mode must be one of none, legacy, velocity, or bungeeguard")
	}
	return nil
}

func normalizeForwardingMode(mode *string) string {
	if mode == nil || strings.TrimSpace(*mode) == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*mode))
}

func normalizeForwardingModeValue(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return ""
	}
	if !isValidForwardingMode(mode) {
		return "legacy"
	}
	return mode
}

func isValidForwardingMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none", "legacy", "velocity", "bungeeguard":
		return true
	default:
		return false
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
	if len([]rune(strings.TrimSpace(in.Target))) > 256 {
		return fmt.Errorf("target is too long")
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

func validatePolicyInput(in PolicyInput) error {
	for name, value := range map[string]*int{
		"ip_burst_10s":   in.IPBurst10s,
		"ip_burst_60s":   in.IPBurst60s,
		"name_burst_10s": in.NameBurst10s,
		"name_burst_60s": in.NameBurst60s,
	} {
		if value != nil && (*value < 0 || *value > 1_000_000) {
			return fmt.Errorf("%s must be between 0 and 1000000", name)
		}
	}
	if in.KickMessage != nil && len([]rune(*in.KickMessage)) > 512 {
		return fmt.Errorf("kick_message is too long")
	}
	if in.GeoMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*in.GeoMode))
		if mode != "disabled" && mode != "allow" && mode != "deny" {
			return fmt.Errorf("invalid geo mode")
		}
	}
	if in.GeoList != nil && len(in.GeoList) > 256 {
		return fmt.Errorf("geo_list is too large")
	}
	return nil
}

func normalizeNickname(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizeCountryCodes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		code := strings.ToUpper(strings.TrimSpace(v))
		if len(code) != 2 {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func (s *Store) getServerPolicy(ctx context.Context, serverID *int) (*PolicyDTO, error) {
	if _, err := s.preloadServerPolicy(ctx, serverIDValue(serverID)); serverID != nil && err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
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
	_ = ctx
	scope := "global"
	if serverID != nil {
		scope = "server"
	}
	if s.dist != nil {
		countVal, err := s.dist.IncrWindow(ctx, scope, serverID, kind, key, windowSec, record)
		if err == nil {
			return countVal, nil
		}
	}
	countVal := s.counters.bump(scope, serverID, kind, key, windowSec, record)
	return countVal, nil
}

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
	}()
	safeDetails, ok := sanitizeAuditValue(details).(map[string]any)
	if !ok {
		safeDetails = map[string]any{}
	}
	details = safeDetails
	b := s.client.Audit.Create().SetEvent(event).SetDetails(details)
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
		s.dist.PublishInvalidate(ctx)
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
