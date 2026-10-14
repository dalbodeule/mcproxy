package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds runtime configuration for mcproxy.
type Config struct {
	HTTPAddr                        string
	AdminToken                      string
	AdminActorSecret                string
	GeoIPPath                       string
	GeoIPASNPath                    string
	AdminThrottle                   int
	AdminAllowedOrigins             []string
	TrustProxyHeaders               bool
	TrustedProxyCIDRs               []string
	RedisAddr                       string
	RedisPassword                   string
	RedisTLS                        bool
	RedisChannel                    string
	RedisFailClosed                 bool
	LokiHost                        string
	LogIdentify                     string
	PingCacheTTL                    int
	PingFallbackMotd                string
	PingOfflineMotd                 string
	PingFallbackFavicon             string
	GateBind                        string
	GateOnlineMode                  bool
	GateProxyProtocol               bool
	GateProxyProtocolBackend        bool
	GateProxyProtocolTrustedProxies []string
	GateMaxConcurrentLoginsPerIP    int
	GateForwardingMode              string
	GateVelocitySecret              string
	DBDriver                        string // "sqlite" (default) or "postgres"
	DSN                             string // full connection string for the chosen driver
	loadError                       error
}

// LoadFromEnv reads configuration from environment variables with sane defaults.
func LoadFromEnv() Config {
	_ = godotenv.Load()

	httpAddr := getenv("MCPROXY_HTTP_ADDR", "127.0.0.1:8080")
	adminToken := getenvSecret("MCPROXY_ADMIN_TOKEN")
	adminActorSecret := getenvSecret("MCPROXY_ADMIN_ACTOR_SECRET")
	geoPath := getenv("MCPROXY_GEOIP_PATH", "")
	geoASNPath := getenv("MCPROXY_GEOIP_ASN_PATH", "")
	adminThrottle := getenvInt("MCPROXY_ADMIN_THROTTLE", 32)
	adminAllowedOrigins := splitCSV(os.Getenv("MCPROXY_ADMIN_ALLOWED_ORIGINS"))
	trustProxyHeaders := getenvBool("MCPROXY_TRUST_PROXY_HEADERS", false)
	trustedProxyCIDRs := splitCSV(os.Getenv("MCPROXY_TRUSTED_PROXY_CIDRS"))
	redisAddr := getenv("MCPROXY_REDIS_ADDR", "")
	redisPassword := getenvSecret("MCPROXY_REDIS_PASSWORD")
	redisTLS := getenvBool("MCPROXY_REDIS_TLS", false)
	redisChannel := getenv("MCPROXY_REDIS_CHANNEL", "mcproxy:events")
	redisFailClosed := getenvBool("MCPROXY_REDIS_FAIL_CLOSED", false)
	lokiHost := getenv("MCPROXY_LOKI_HOST", "")
	logIdentify := getenv("MCPROXY_LOG_IDENTIFY", "local")
	pingCacheTTL := getenvInt("MCPROXY_PING_CACHE_TTL_SEC", 300)
	pingFallbackMotd := getenv("MCPROXY_PING_FALLBACK_MOTD", "mcproxy available")
	pingOfflineMotd := getenv("MCPROXY_PING_OFFLINE_MOTD", "Server unavailable")
	pingFallbackFavicon := getenv("MCPROXY_PING_FALLBACK_FAVICON", "")
	gateBind := getenv("MCPROXY_GATE_BIND", "0.0.0.0:25565")
	gateOnlineMode := getenvBool("MCPROXY_GATE_ONLINE_MODE", false)
	gateProxyProtocol := getenvBool("MCPROXY_GATE_PROXY_PROTOCOL", false)
	gateProxyProtocolBackend := getenvBool("MCPROXY_GATE_PROXY_PROTOCOL_BACKEND", false)
	gateProxyProtocolTrustedProxies := splitCSV(os.Getenv("MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES"))
	gateMaxConcurrentLoginsPerIP := getenvInt("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP", 0)
	gateForwardingMode := strings.ToLower(strings.TrimSpace(getenv("MCPROXY_GATE_FORWARDING_MODE", "legacy")))
	gateVelocitySecret := getenvSecret("MCPROXY_GATE_VELOCITY_SECRET")
	driver := strings.ToLower(strings.TrimSpace(getenv("MCPROXY_DB_DRIVER", "sqlite")))
	invalidSettings := make([]string, 0, 4)
	for _, key := range []string{"MCPROXY_GATE_ONLINE_MODE", "MCPROXY_GATE_PROXY_PROTOCOL", "MCPROXY_GATE_PROXY_PROTOCOL_BACKEND", "MCPROXY_REDIS_TLS", "MCPROXY_REDIS_FAIL_CLOSED", "MCPROXY_TRUST_PROXY_HEADERS"} {
		if !validBoolEnv(key) {
			invalidSettings = append(invalidSettings, key)
		}
	}
	if raw := strings.TrimSpace(os.Getenv("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP")); raw != "" {
		var value int
		if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value < 0 {
			invalidSettings = append(invalidSettings, "MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP")
		}
	}
	if adminActorSecret != "" && len(adminActorSecret) < 32 {
		invalidSettings = append(invalidSettings, "MCPROXY_ADMIN_ACTOR_SECRET")
	}
	if gateProxyProtocol {
		if len(gateProxyProtocolTrustedProxies) == 0 {
			invalidSettings = append(invalidSettings, "MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES")
		} else {
			for _, network := range gateProxyProtocolTrustedProxies {
				if !validNetwork(network) {
					invalidSettings = append(invalidSettings, "MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES")
					break
				}
			}
		}
	}

	var dsn string
	switch driver {
	case "sqlite":
		dsn = sqliteDSN(getenv("MCPROXY_SQLITE_PATH", filepath.Join("data", "mcproxy.db")))
	case "postgres":
		dsn = getenvSecret("MCPROXY_POSTGRES_DSN")
	default:
		// Preserve an unsupported value so Store.Open/Migrate fails explicitly.
		// Silently falling back to SQLite could start a deployment against a new
		// local database after a typo in MCPROXY_DB_DRIVER.
		dsn = ""
	}

	var loadError error
	if len(invalidSettings) > 0 {
		loadError = fmt.Errorf("invalid configuration value(s): %s", strings.Join(invalidSettings, ", "))
	}
	return Config{
		HTTPAddr:                        httpAddr,
		AdminToken:                      adminToken,
		AdminActorSecret:                adminActorSecret,
		GeoIPPath:                       geoPath,
		GeoIPASNPath:                    geoASNPath,
		AdminThrottle:                   adminThrottle,
		AdminAllowedOrigins:             adminAllowedOrigins,
		TrustProxyHeaders:               trustProxyHeaders,
		TrustedProxyCIDRs:               trustedProxyCIDRs,
		RedisAddr:                       redisAddr,
		RedisPassword:                   redisPassword,
		RedisTLS:                        redisTLS,
		RedisChannel:                    redisChannel,
		RedisFailClosed:                 redisFailClosed,
		LokiHost:                        lokiHost,
		LogIdentify:                     logIdentify,
		PingCacheTTL:                    pingCacheTTL,
		PingFallbackMotd:                pingFallbackMotd,
		PingOfflineMotd:                 pingOfflineMotd,
		PingFallbackFavicon:             pingFallbackFavicon,
		GateBind:                        gateBind,
		GateOnlineMode:                  gateOnlineMode,
		GateProxyProtocol:               gateProxyProtocol,
		GateProxyProtocolBackend:        gateProxyProtocolBackend,
		GateProxyProtocolTrustedProxies: gateProxyProtocolTrustedProxies,
		GateMaxConcurrentLoginsPerIP:    gateMaxConcurrentLoginsPerIP,
		GateForwardingMode:              gateForwardingMode,
		GateVelocitySecret:              gateVelocitySecret,
		DBDriver:                        driver,
		DSN:                             dsn,
		loadError:                       loadError,
	}
}

// Validate reports malformed environment values before opening databases or
// starting the data plane. In particular, security booleans must not silently
// fall back to a weaker default after a typo.
func (c Config) Validate() error {
	return c.loadError
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

func validBoolEnv(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return true
	}
	switch v {
	case "1", "true", "yes", "on", "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

func validNetwork(value string) bool {
	value = strings.TrimSpace(value)
	if net.ParseIP(value) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(value)
	return err == nil
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
