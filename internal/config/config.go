package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds runtime configuration for mcproxy.
type Config struct {
	HTTPAddr              string
	AdminToken            string
	GeoIPPath             string
	AdminThrottle         int
	AdminAllowedOrigins   []string
	TrustProxyHeaders     bool
	RedisAddr             string
	RedisPassword         string
	RedisTLS              bool
	RedisChannel          string
	LokiHost              string
	LogIdentify           string
	PingCacheTTL          int
	PingFallbackMotd      string
	PingOfflineMotd       string
	PingFallbackFavicon   string
	GateBind              string
	GateOnlineMode        bool
	GateForwardingMode    string
	GateVelocitySecret    string
	GateBungeeGuardSecret string
	DBDriver              string // "sqlite" (default) or "postgres"
	DSN                   string // full connection string for the chosen driver
}

// LoadFromEnv reads configuration from environment variables with sane defaults.
func LoadFromEnv() Config {
	_ = godotenv.Load()

	httpAddr := getenv("MCPROXY_HTTP_ADDR", "127.0.0.1:8080")
	adminToken := getenvSecret("MCPROXY_ADMIN_TOKEN")
	geoPath := getenv("MCPROXY_GEOIP_PATH", "")
	adminThrottle := getenvInt("MCPROXY_ADMIN_THROTTLE", 32)
	adminAllowedOrigins := splitCSV(os.Getenv("MCPROXY_ADMIN_ALLOWED_ORIGINS"))
	trustProxyHeaders := getenvBool("MCPROXY_TRUST_PROXY_HEADERS", false)
	redisAddr := getenv("MCPROXY_REDIS_ADDR", "")
	redisPassword := getenvSecret("MCPROXY_REDIS_PASSWORD")
	redisTLS := getenvBool("MCPROXY_REDIS_TLS", false)
	redisChannel := getenv("MCPROXY_REDIS_CHANNEL", "mcproxy:events")
	lokiHost := getenv("MCPROXY_LOKI_HOST", "")
	logIdentify := getenv("MCPROXY_LOG_IDENTIFY", "local")
	pingCacheTTL := getenvInt("MCPROXY_PING_CACHE_TTL_SEC", 300)
	pingFallbackMotd := getenv("MCPROXY_PING_FALLBACK_MOTD", "mcproxy available")
	pingOfflineMotd := getenv("MCPROXY_PING_OFFLINE_MOTD", "Server unavailable")
	pingFallbackFavicon := getenv("MCPROXY_PING_FALLBACK_FAVICON", "")
	gateBind := getenv("MCPROXY_GATE_BIND", "0.0.0.0:25565")
	gateOnlineMode := getenvBool("MCPROXY_GATE_ONLINE_MODE", false)
	gateForwardingMode := strings.ToLower(strings.TrimSpace(getenv("MCPROXY_GATE_FORWARDING_MODE", "legacy")))
	gateVelocitySecret := getenvSecret("MCPROXY_GATE_VELOCITY_SECRET")
	gateBungeeGuardSecret := getenvSecret("MCPROXY_GATE_BUNGEGUARD_SECRET")
	driver := getenv("MCPROXY_DB_DRIVER", "sqlite")

	var dsn string
	switch driver {
	case "sqlite":
		dsn = sqliteDSN(getenv("MCPROXY_SQLITE_PATH", filepath.Join("data", "mcproxy.db")))
	case "postgres":
		dsn = getenvSecret("MCPROXY_POSTGRES_DSN")
	default:
		dsn = sqliteDSN(filepath.Join("data", "mcproxy.db"))
		driver = "sqlite"
	}

	return Config{
		HTTPAddr:              httpAddr,
		AdminToken:            adminToken,
		GeoIPPath:             geoPath,
		AdminThrottle:         adminThrottle,
		AdminAllowedOrigins:   adminAllowedOrigins,
		TrustProxyHeaders:     trustProxyHeaders,
		RedisAddr:             redisAddr,
		RedisPassword:         redisPassword,
		RedisTLS:              redisTLS,
		RedisChannel:          redisChannel,
		LokiHost:              lokiHost,
		LogIdentify:           logIdentify,
		PingCacheTTL:          pingCacheTTL,
		PingFallbackMotd:      pingFallbackMotd,
		PingOfflineMotd:       pingOfflineMotd,
		PingFallbackFavicon:   pingFallbackFavicon,
		GateBind:              gateBind,
		GateOnlineMode:        gateOnlineMode,
		GateForwardingMode:    gateForwardingMode,
		GateVelocitySecret:    gateVelocitySecret,
		GateBungeeGuardSecret: gateBungeeGuardSecret,
		DBDriver:              driver,
		DSN:                   dsn,
	}
}

func (c Config) String() string {
	// Never include DSNs or secrets in startup logs; PostgreSQL DSNs commonly contain passwords.
	return fmt.Sprintf("http=%s db=%s geo=%s gate=%s identify=%s", c.HTTPAddr, c.DBDriver, c.GeoIPPath, c.GateBind, c.LogIdentify)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// getenvSecret supports the conventional KEY_FILE form used by Docker and
// Kubernetes Secret mounts. An explicitly supplied environment value wins so
// local development and existing deployments remain backwards compatible.
func getenvSecret(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	file := strings.TrimSpace(os.Getenv(key + "_FILE"))
	if file == "" {
		return ""
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
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

func sqliteDSN(path string) string {
	if path == "" {
		return "file:data/mcproxy.db?_pragma=foreign_keys(1)"
	}
	if strings.HasPrefix(path, "file:") {
		if strings.Contains(path, "_pragma=foreign_keys(1)") {
			return path
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		return path + sep + "_pragma=foreign_keys(1)"
	}
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		return "file:" + url.PathEscape(parts[0]) + "?" + parts[1] + "&_pragma=foreign_keys(1)"
	}
	return "file:" + url.PathEscape(path) + "?_pragma=foreign_keys(1)"
}
