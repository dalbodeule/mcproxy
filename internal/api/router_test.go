package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"mcproxy/internal/config"
	"mcproxy/internal/store"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "mcproxy-test.db") + "?_fk=1"
	cfg := config.Config{
		HTTPAddr:      "127.0.0.1:8080",
		AdminToken:    "test-token",
		AdminThrottle: 32,
		DBDriver:      "sqlite",
		DSN:           dsn,
		LogIdentify:   "test",
	}
	st, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewRouter(cfg, Dependencies{Store: st, Geo: nil})
}

func authReq(method, path string, body any) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestHealth(t *testing.T) {
	r := newTestRouter(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestServerLifecycleAndServerPolicy(t *testing.T) {
	r := newTestRouter(t)

	// create server
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/servers/", map[string]any{
		"name":     "lobby",
		"upstream": "127.0.0.1:25566",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	// update server policy
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodPut, "/api/v1/policy/servers/1", map[string]any{
		"ip_burst_10s":   7,
		"name_burst_60s": 12,
		"geo_mode":       "allow",
		"geo_list":       []string{"KR", "JP"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("put server policy expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// get server policy
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, "/api/v1/policy/servers/1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server policy expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var policy map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &policy)
	if int(policy["id"].(float64)) == 0 || policy["scope"] != "server" {
		t.Fatalf("unexpected server policy response: %v", policy)
	}

	// get server by id
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, "/api/v1/servers/1/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if id != 1 {
		t.Fatalf("expected created server id 1, got %d", id)
	}
}

func TestGeoPolicyEndpoints(t *testing.T) {
	r := newTestRouter(t)

	// global geo upsert
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodPut, "/api/v1/policy/geo", map[string]any{
		"mode":      "deny",
		"countries": []string{"RU", "CN", "ru"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("put global geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// global geo get
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, "/api/v1/policy/geo", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get global geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var geo map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &geo)
	if geo["mode"] != "deny" {
		t.Fatalf("expected deny mode, got %v", geo)
	}

	// create server then set server-scoped geo policy
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodPost, "/api/v1/servers/", map[string]any{
		"name":     "survival",
		"upstream": "127.0.0.1:25567",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodPut, "/api/v1/policy/geo?server_id=1", map[string]any{
		"mode":      "allow",
		"countries": []string{"KR"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("put server geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(http.MethodGet, "/api/v1/policy/geo?server_id=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}
