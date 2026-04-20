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
	"time"

	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib" // postgres driver for database/sql
	_ "modernc.org/sqlite"             // sqlite driver for database/sql

	"mcproxy/internal/config"
	"mcproxy/internal/ent"
	"mcproxy/internal/ent/accesspolicy"
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
	up := time.Since(s.startedAt) / time.Second
	return Stats{UptimeSeconds: int64(up), Servers: servers, Policies: policies, Rules: rules, Blocks: 0}, nil
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
