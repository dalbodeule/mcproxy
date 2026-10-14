package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // postgres driver for database/sql
	_ "modernc.org/sqlite"             // sqlite driver for database/sql

	"mcproxy/internal/config"
	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
	"mcproxy/internal/ent/audit"
	"mcproxy/internal/ent/rule"
	"mcproxy/internal/ent/server"
)

type Store struct {
	db                        *sql.DB
	client                    *ent.Client
	startedAt                 time.Time
	cache                     *snapshotCache
	counters                  *bucketCounter
	audits                    *auditWriter
	dist                      *distributedCoordinator
	distributedFailClosed     bool
	invalidations             chan struct{}
	blockMu                   sync.RWMutex
	closeOnce                 sync.Once
	closeErr                  error
	blockReasons              map[string]int64
	serverAttemptsMu          sync.RWMutex
	serverAttempts            map[int]int64
	attempts                  atomic.Int64
	allowed                   atomic.Int64
	decisionBlocks            atomic.Int64
	evaluationDurationNS      atomic.Int64
	evaluationDurationCount   atomic.Int64
	statsDurationNS           atomic.Int64
	statsDurationCount        atomic.Int64
	snapshotRefreshes         atomic.Uint64
	snapshotRefreshDurationNS atomic.Int64
	distributedErrors         atomic.Int64
	distributedFallbacks      atomic.Int64
	distributedPublishErrors  atomic.Int64
	distributedHealthy        atomic.Bool
	auditWriteErrors          atomic.Int64
}

type Stats struct {
	UptimeSeconds  int64            `json:"uptime_seconds"`
	Servers        int              `json:"servers"`
	EnabledServers int              `json:"enabled_servers"`
	Policies       int              `json:"policies"`
	Rules          int              `json:"rules"`
	Counters       int              `json:"counters"`
	Blocks         int64            `json:"blocks"`
	Attempts       int64            `json:"attempts"`
	Allowed        int64            `json:"allowed"`
	DecisionBlocks int64            `json:"decision_blocks"`
	BlockReasons   map[string]int64 `json:"block_reasons,omitempty"`
}

type CacheMetrics struct {
	Hits       uint64
	Misses     uint64
	AgeSeconds float64
}

type DistributedMetrics struct {
	Enabled       bool
	Healthy       bool
	FailClosed    bool
	Errors        int64
	Fallbacks     int64
	PublishErrors int64
}

type EvaluationMetrics struct {
	DurationSeconds         float64
	DurationCount           int64
	ServerAttempts          map[int]int64
	SnapshotRefreshes       uint64
	SnapshotDurationSeconds float64
}

type DatabaseMetrics struct {
	OpenConnections int
	InUse           int
	Idle            int
	WaitCount       int64
	WaitDuration    time.Duration
}

type StoreLatencyMetrics struct {
	StatsDurationSeconds float64
	StatsDurationCount   int64
}

// AuditMetrics exposes audit persistence failures without including event
// names, actors, or payloads in metrics.
type AuditMetrics struct {
	WriteErrors int64
}

func (s *Store) AuditMetrics() AuditMetrics {
	if s == nil {
		return AuditMetrics{}
	}
	return AuditMetrics{WriteErrors: s.auditWriteErrors.Load()}
}

// StoreLatencyMetrics exposes control-plane database-backed operation latency
// without including query text, DSNs, or high-cardinality labels.
func (s *Store) StoreLatencyMetrics() StoreLatencyMetrics {
	if s == nil {
		return StoreLatencyMetrics{}
	}
	return StoreLatencyMetrics{
		StatsDurationSeconds: float64(s.statsDurationNS.Load()) / float64(time.Second),
		StatsDurationCount:   s.statsDurationCount.Load(),
	}
}

// DatabaseMetrics exposes pool pressure without exposing DSNs or query data.
func (s *Store) DatabaseMetrics() DatabaseMetrics {
	if s == nil || s.db == nil {
		return DatabaseMetrics{}
	}
	stats := s.db.Stats()
	return DatabaseMetrics{
		OpenConnections: stats.OpenConnections,
		InUse:           stats.InUse,
		Idle:            stats.Idle,
		WaitCount:       stats.WaitCount,
		WaitDuration:    stats.WaitDuration,
	}
}

// EvaluationMetrics exposes process-local latency and per-server counters for
// diagnosing data-plane load without adding labels for IPs or usernames.
func (s *Store) EvaluationMetrics() EvaluationMetrics {
	if s == nil {
		return EvaluationMetrics{}
	}
	s.serverAttemptsMu.RLock()
	serverAttempts := make(map[int]int64, len(s.serverAttempts))
	for serverID, count := range s.serverAttempts {
		serverAttempts[serverID] = count
	}
	s.serverAttemptsMu.RUnlock()
	return EvaluationMetrics{
		DurationSeconds:         float64(s.evaluationDurationNS.Load()) / float64(time.Second),
		DurationCount:           s.evaluationDurationCount.Load(),
		ServerAttempts:          serverAttempts,
		SnapshotRefreshes:       s.snapshotRefreshes.Load(),
		SnapshotDurationSeconds: float64(s.snapshotRefreshDurationNS.Load()) / float64(time.Second),
	}
}

func (s *Store) recordServerAttempt(serverID *int) {
	if serverID == nil {
		return
	}
	s.serverAttemptsMu.Lock()
	if s.serverAttempts == nil {
		s.serverAttempts = make(map[int]int64)
	}
	s.serverAttempts[*serverID]++
	s.serverAttemptsMu.Unlock()
}

// DistributedMetrics reports Redis degradation without exposing connection
// details. Local fallback keeps the data plane available, but cross-instance
// enforcement may be weaker until Redis recovers.
func (s *Store) DistributedMetrics() DistributedMetrics {
	if s == nil {
		return DistributedMetrics{}
	}
	return DistributedMetrics{
		Enabled:       s.dist != nil,
		Healthy:       s.dist == nil || s.distributedHealthy.Load(),
		FailClosed:    s.distributedFailClosed,
		Errors:        s.distributedErrors.Load(),
		Fallbacks:     s.distributedFallbacks.Load(),
		PublishErrors: s.distributedPublishErrors.Load(),
	}
}

// CheckDistributed actively verifies Redis for fail-closed readiness. The
// health flag is otherwise updated by normal counter/publish operations, which
// is not sufficient when an idle instance loses Redis connectivity.
func (s *Store) CheckDistributed(ctx context.Context) error {
	if s == nil || s.dist == nil {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.dist.Ping(checkCtx); err != nil {
		s.distributedErrors.Add(1)
		s.distributedHealthy.Store(false)
		return err
	}
	s.distributedHealthy.Store(true)
	return nil
}

// CacheMetrics returns low-cardinality snapshot-cache telemetry without
// exposing policy or rule contents.
func (s *Store) CacheMetrics() CacheMetrics {
	if s == nil || s.cache == nil {
		return CacheMetrics{}
	}
	m := s.cache.metrics()
	return CacheMetrics{Hits: m.Hits, Misses: m.Misses, AgeSeconds: m.AgeSeconds}
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
	ASN      string `json:"asn,omitempty"`
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
	releaseSchemaLock, err := acquireSchemaLock(ctx, db, driver)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("acquire schema migration lock: %w", err)
	}

	client := ent.NewClient(ent.Driver(entsql.OpenDB(entDrv, db)))
	if err := client.Schema.Create(ctx); err != nil {
		_ = releaseSchemaLock()
		_ = client.Close()
		_ = db.Close()
		return nil, fmt.Errorf("ent schema create: %w", err)
	}
	if err := releaseSchemaLock(); err != nil {
		_ = client.Close()
		_ = db.Close()
		return nil, fmt.Errorf("release schema migration lock: %w", err)
	}

	s := &Store{db: db, client: client, startedAt: time.Now(), cache: newSnapshotCache(), counters: newBucketCounter(), blockReasons: make(map[string]int64), serverAttempts: make(map[int]int64), invalidations: make(chan struct{}, 1), distributedFailClosed: cfg.RedisFailClosed}
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
	s.distributedHealthy.Store(true)
	s.audits = newAuditWriter(s)
	_ = s.refreshSnapshots(ctx)
	return s, nil
}

const schemaMigrationAdvisoryLock int64 = 0x6d6370726f7879 // "mcproxy"

// acquireSchemaLock serializes Ent's additive schema creation across HA
// replicas. PostgreSQL advisory locks are session-scoped, so the dedicated
// connection remains open until Schema.Create has completed. SQLite uses its
// normal file locking and needs no separate connection here.
func acquireSchemaLock(ctx context.Context, db *sql.DB, driver string) (func() error, error) {
	if driver != "postgres" {
		return func() error { return nil }, nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schemaMigrationAdvisoryLock); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return func() error {
		_, unlockErr := conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaMigrationAdvisoryLock)
		closeErr := conn.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
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
	s.closeOnce.Do(func() {
		// Always attempt every close step. A Redis shutdown error must not
		// prevent the database and Ent client from being released.
		if s.audits != nil {
			s.audits.stop()
		}
		if s.dist != nil {
			if err := s.dist.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.client != nil {
			if err := s.client.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
		if s.db != nil {
			if err := s.db.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

// Stats returns runtime and persisted entity counts.
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	started := time.Now()
	defer func() {
		s.statsDurationNS.Add(time.Since(started).Nanoseconds())
		s.statsDurationCount.Add(1)
	}()
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
			s.distributedErrors.Add(1)
			s.distributedFallbacks.Add(1)
			s.distributedHealthy.Store(false)
		} else {
			counters = distributedCounters
			s.distributedHealthy.Store(true)
		}
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
	return Stats{UptimeSeconds: int64(up), Servers: servers, EnabledServers: enabledServers, Policies: policies, Rules: rules, Counters: counters, Blocks: int64(blocked), Attempts: s.attempts.Load(), Allowed: s.allowed.Load(), DecisionBlocks: s.decisionBlocks.Load(), BlockReasons: blockReasons}, nil
}

// Health verifies that the backing database is reachable. It is intentionally
// separate from Stats so readiness checks do not enumerate application tables.
func (s *Store) Health(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}
	return s.db.PingContext(ctx)
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
