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

func TestSplitCSVTrimsAndDropsEmptyValues(t *testing.T) {
	got := splitCSV(" https://admin.example , ,https://panel.example ")
	if len(got) != 2 || got[0] != "https://admin.example" || got[1] != "https://panel.example" {
		t.Fatalf("unexpected origins: %#v", got)
	}
}
