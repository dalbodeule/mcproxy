package testutil

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"mcproxy/internal/config"
	"mcproxy/internal/store"
)

func NewTestConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		HTTPAddr:      "127.0.0.1:8080",
		AdminToken:    "test-token",
		AdminThrottle: 32,
		DBDriver:      "sqlite",
		DSN:           "file:" + filepath.Join(t.TempDir(), "mcproxy-test.db") + "?_pragma=foreign_keys(1)",
		LogIdentify:   "test",
	}
}

func NewTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), NewTestConfig(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func AuthRequest(t *testing.T, method, path string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}
