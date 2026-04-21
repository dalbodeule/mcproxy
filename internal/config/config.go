package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds runtime configuration for mcproxy.
type Config struct {
	HTTPAddr       string
	AdminToken     string
	GeoIPPath      string
	AdminThrottle  int
	RedisAddr      string
	RedisChannel   string
	GateBind       string
	GateOnlineMode bool
	DBDriver       string // "sqlite" (default) or "postgres"
	DSN            string // full connection string for the chosen driver
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
	adminThrottle := getenvInt("MCPROXY_ADMIN_THROTTLE", 32)
	redisAddr := getenv("MCPROXY_REDIS_ADDR", "")
	redisChannel := getenv("MCPROXY_REDIS_CHANNEL", "mcproxy:events")
	gateBind := getenv("MCPROXY_GATE_BIND", "0.0.0.0:25565")
	gateOnlineMode := getenvBool("MCPROXY_GATE_ONLINE_MODE", false)
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
		HTTPAddr:       httpAddr,
		AdminToken:     adminToken,
		GeoIPPath:      geoPath,
		AdminThrottle:  adminThrottle,
		RedisAddr:      redisAddr,
		RedisChannel:   redisChannel,
		GateBind:       gateBind,
		GateOnlineMode: gateOnlineMode,
		DBDriver:       driver,
		DSN:            dsn,
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

func getenvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var out int
	if _, err := fmt.Sscanf(v, "%d", &out); err != nil || out <= 0 {
		return def
	}
	return out
}

func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
