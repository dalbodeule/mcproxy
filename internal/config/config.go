package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds runtime configuration for mcproxy.
type Config struct {
	HTTPAddr   string
	AdminToken string
	GeoIPPath  string
	DBDriver   string // "sqlite" (default) or "postgres"
	DSN        string // full connection string for the chosen driver
}

// LoadFromEnv reads configuration from environment variables with sane defaults.
//
// Env vars:
// - MCPROXY_HTTP_ADDR (default "127.0.0.1:8080")
// - MCPROXY_ADMIN_TOKEN (required for /api/v1 management endpoints)
// - MCPROXY_GEOIP_PATH (optional)
// - MCPROXY_DB_DRIVER (sqlite|postgres; default sqlite)
// - MCPROXY_SQLITE_PATH (default data/mcproxy.db)
// - MCPROXY_POSTGRES_DSN (postgres connection string; required if DB_DRIVER=postgres)
func LoadFromEnv() Config {
	httpAddr := getenv("MCPROXY_HTTP_ADDR", "127.0.0.1:8080")
	adminToken := getenv("MCPROXY_ADMIN_TOKEN", "")
	geoPath := getenv("MCPROXY_GEOIP_PATH", "")
	driver := getenv("MCPROXY_DB_DRIVER", "sqlite")

	var dsn string
	switch driver {
	case "sqlite":
		p := getenv("MCPROXY_SQLITE_PATH", filepath.Join("data", "mcproxy.db"))
		// modernc.org/sqlite driver name is "sqlite"; DSN is a file path or URI.
		// Ensure directory exists lazily in store.Open.
		dsn = p
	case "postgres":
		dsn = getenv("MCPROXY_POSTGRES_DSN", "")
	default:
		// Fallback to sqlite if unknown driver provided.
		p := filepath.Join("data", "mcproxy.db")
		driver = "sqlite"
		dsn = p
	}

	return Config{
		HTTPAddr:   httpAddr,
		AdminToken: adminToken,
		GeoIPPath:  geoPath,
		DBDriver:   driver,
		DSN:        dsn,
	}
}

func (c Config) String() string {
	// Avoid logging DSN to prevent credential leakage
	return fmt.Sprintf("http=%s db=%s geo=%s", c.HTTPAddr, c.DBDriver, c.GeoIPPath)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
