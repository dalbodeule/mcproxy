package config

import (
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

func TestSplitCSVTrimsAndDropsEmptyValues(t *testing.T) {
	got := splitCSV(" https://admin.example , ,https://panel.example ")
	if len(got) != 2 || got[0] != "https://admin.example" || got[1] != "https://panel.example" {
		t.Fatalf("unexpected origins: %#v", got)
	}
}
