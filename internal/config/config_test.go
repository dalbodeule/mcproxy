package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigStringDoesNotExposeDSN(t *testing.T) {
	c := Config{
		HTTPAddr: "127.0.0.1:8080",
		DBDriver: "postgres",
		DSN:      "postgres://admin:super-secret@example/db?sslmode=disable",
	}
	got := c.String()
	if strings.Contains(got, "super-secret") || strings.Contains(got, "postgres://") {
		t.Fatalf("config string leaked database credentials: %q", got)
	}
}

func TestGetenvSecretPrefersEnvironmentAndSupportsFile(t *testing.T) {
	key := "MCPROXY_TEST_SECRET"
	file := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(file, []byte(" from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key, "")
	t.Setenv(key+"_FILE", file)
	if got := getenvSecret(key); got != "from-file" {
		t.Fatalf("file secret = %q, want from-file", got)
	}
	t.Setenv(key, "from-env")
	if got := getenvSecret(key); got != "from-env" {
		t.Fatalf("environment secret = %q, want from-env", got)
	}
}

func TestLoadFromEnvAdminActorSecret(t *testing.T) {
	t.Setenv("MCPROXY_ADMIN_ACTOR_SECRET", "panel-audit-secret")
	cfg := LoadFromEnv()
	if got := cfg.AdminActorSecret; got != "panel-audit-secret" {
		t.Fatalf("admin actor secret = %q", got)
	}
	if cfg.Validate() == nil {
		t.Fatal("short admin actor secret was accepted")
	}
	t.Setenv("MCPROXY_ADMIN_ACTOR_SECRET", "panel-audit-secret-012345678901234567890")
	if cfg = LoadFromEnv(); cfg.Validate() != nil {
		t.Fatalf("strong admin actor secret was rejected: %v", cfg.Validate())
	}
}

func TestSplitCSVTrimsAndDropsEmptyValues(t *testing.T) {
	got := splitCSV(" https://admin.example , ,https://panel.example ")
	if len(got) != 2 || got[0] != "https://admin.example" || got[1] != "https://panel.example" {
		t.Fatalf("unexpected origins: %#v", got)
	}
}

func TestLoadFromEnvRedisFailClosed(t *testing.T) {
	t.Setenv("MCPROXY_REDIS_FAIL_CLOSED", "true")
	cfg := LoadFromEnv()
	if !cfg.RedisFailClosed {
		t.Fatal("MCPROXY_REDIS_FAIL_CLOSED=true was not loaded")
	}
	t.Setenv("MCPROXY_REDIS_FAIL_CLOSED", "false")
	cfg = LoadFromEnv()
	if cfg.RedisFailClosed {
		t.Fatal("MCPROXY_REDIS_FAIL_CLOSED=false was not loaded")
	}
}

func TestLoadFromEnvRejectsUnknownDatabaseDriverWithoutSQLiteFallback(t *testing.T) {
	t.Setenv("MCPROXY_DB_DRIVER", "postgresql typo")
	cfg := LoadFromEnv()
	if cfg.DBDriver != "postgresql typo" {
		t.Fatalf("unknown database driver was rewritten: %q", cfg.DBDriver)
	}
	if cfg.DSN != "" {
		t.Fatalf("unknown database driver received a fallback DSN: %q", cfg.DSN)
	}

	t.Setenv("MCPROXY_DB_DRIVER", " POSTGRES ")
	t.Setenv("MCPROXY_POSTGRES_DSN", "postgres://db/mcproxy")
	cfg = LoadFromEnv()
	if cfg.DBDriver != "postgres" || cfg.DSN != "postgres://db/mcproxy" {
		t.Fatalf("normalized PostgreSQL driver was not loaded: driver=%q dsn=%q", cfg.DBDriver, cfg.DSN)
	}
}

func TestLoadFromEnvGateProxyProtocol(t *testing.T) {
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL", "true")
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL_BACKEND", "true")
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES", "10.0.0.0/8, 192.0.2.10")
	t.Setenv("MCPROXY_TRUST_PROXY_HEADERS", "true")
	t.Setenv("MCPROXY_TRUSTED_PROXY_CIDRS", "127.0.0.0/8, 10.0.0.0/8")
	cfg := LoadFromEnv()
	if !cfg.GateProxyProtocol || !cfg.GateProxyProtocolBackend {
		t.Fatal("Gate PROXY protocol flags were not loaded")
	}
	if !cfg.TrustProxyHeaders || len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("HTTP trusted proxy settings were not loaded: %#v", cfg)
	}
	if len(cfg.GateProxyProtocolTrustedProxies) != 2 || cfg.GateProxyProtocolTrustedProxies[0] != "10.0.0.0/8" || cfg.GateProxyProtocolTrustedProxies[1] != "192.0.2.10" {
		t.Fatalf("unexpected trusted PROXY protocol proxies: %#v", cfg.GateProxyProtocolTrustedProxies)
	}
}

func TestLoadFromEnvGateConcurrentLoginLimit(t *testing.T) {
	t.Setenv("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP", "3")
	if got := LoadFromEnv().GateMaxConcurrentLoginsPerIP; got != 3 {
		t.Fatalf("concurrent login limit = %d, want 3", got)
	}
	t.Setenv("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP", "0")
	if got := LoadFromEnv().GateMaxConcurrentLoginsPerIP; got != 0 {
		t.Fatalf("zero concurrent login limit = %d, want disabled", got)
	}
}

func TestLoadFromEnvRejectsMalformedSecuritySettings(t *testing.T) {
	t.Setenv("MCPROXY_REDIS_FAIL_CLOSED", "definitely")
	if err := LoadFromEnv().Validate(); err == nil {
		t.Fatal("malformed Redis fail-closed value was accepted")
	}
	t.Setenv("MCPROXY_REDIS_FAIL_CLOSED", "true")
	t.Setenv("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP", "not-a-number")
	if err := LoadFromEnv().Validate(); err == nil {
		t.Fatal("malformed login limit was accepted")
	}
	t.Setenv("MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP", "0")
	if err := LoadFromEnv().Validate(); err != nil {
		t.Fatalf("zero login limit should disable the optional limit: %v", err)
	}
}

func TestLoadFromEnvRequiresTrustedProxyProtocolNetworks(t *testing.T) {
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL", "true")
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES", "")
	if err := LoadFromEnv().Validate(); err == nil {
		t.Fatal("PROXY protocol was accepted without trusted networks")
	}
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES", "not-an-ip")
	if err := LoadFromEnv().Validate(); err == nil {
		t.Fatal("invalid PROXY protocol trusted network was accepted")
	}
	t.Setenv("MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES", "10.0.0.0/8,192.0.2.10")
	if err := LoadFromEnv().Validate(); err != nil {
		t.Fatalf("valid PROXY protocol trusted networks rejected: %v", err)
	}
}

func TestLoadFromEnvGeoIPASNPath(t *testing.T) {
	t.Setenv("MCPROXY_GEOIP_ASN_PATH", "/data/GeoLite2-ASN.mmdb")
	if got := LoadFromEnv().GeoIPASNPath; got != "/data/GeoLite2-ASN.mmdb" {
		t.Fatalf("ASN GeoIP path = %q", got)
	}
}
