package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcproxy/internal/testutil"
)

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	cfg := testutil.NewTestConfig(t)
	st := testutil.NewTestStore(t)
	return NewRouter(cfg, Dependencies{Store: st, Geo: nil})
}

func authReq(t *testing.T, method, path string, body any) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header = testutil.AuthRequest(t, method, path, nil).Header.Clone()
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

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/audits?limit=20", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("audit list expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReadyIncludesGateStateWhenConfigured(t *testing.T) {
	cfg := testutil.NewTestConfig(t)
	st := testutil.NewTestStore(t)
	r := NewRouter(cfg, Dependencies{Store: st, GateReady: func() bool { return false }})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "gate_not_ready") {
		t.Fatalf("expected Gate readiness failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	r = NewRouter(cfg, Dependencies{Store: st, GateReady: func() bool { return true }, GateStats: func() (int, int) { return 4, 2 }})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected ready response when Gate is ready, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mcproxy_gate_ready 1") || !strings.Contains(rec.Body.String(), "mcproxy_gate_players 4") || !strings.Contains(rec.Body.String(), "mcproxy_gate_backends 2") {
		t.Fatalf("expected Gate metric, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminAuthFailureRateLimit(t *testing.T) {
	r := newTestRouter(t)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer incorrect-token")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d expected 401, got %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer incorrect-token")
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st failure expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("expected Retry-After header, got %q", rec.Header().Get("Retry-After"))
	}
}

func TestServerLifecycleAndServerPolicy(t *testing.T) {
	r := newTestRouter(t)

	// create server
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPost, "/api/v1/servers/", map[string]any{
		"name":            "lobby",
		"upstream":        "127.0.0.1:25566",
		"public_host":     "play.example.test",
		"public_addr":     "play.example.test:25565",
		"ping_motd":       "Lobby network",
		"priority":        7,
		"forwarding_mode": "bungeeguard",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	// update server policy
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPut, "/api/v1/policy/servers/1", map[string]any{
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
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/policy/servers/1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server policy expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var policy map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &policy)
	if int(policy["id"].(float64)) == 0 || policy["scope"] != "server" {
		t.Fatalf("unexpected server policy response: %v", policy)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodDelete, "/api/v1/policy/servers/1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete server policy expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/policy/servers/1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"inherited":true`) {
		t.Fatalf("expected inherited server policy after reset, got %d body=%s", rec.Code, rec.Body.String())
	}

	// get server by id
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/servers/1/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if id != 1 {
		t.Fatalf("expected created server id 1, got %d", id)
	}
	if created["public_host"] != "play.example.test" || created["public_addr"] != "play.example.test:25565" || int(created["priority"].(float64)) != 7 {
		t.Fatalf("expected routing fields in server response, got %v", created)
	}
	if created["ping_motd"] != "Lobby network" {
		t.Fatalf("expected server ping MOTD in response, got %v", created["ping_motd"])
	}
	if created["forwarding_mode"] != "bungeeguard" {
		t.Fatalf("expected per-server forwarding mode, got %v", created["forwarding_mode"])
	}
	if token, ok := created["bungeeguard_allowed_token"].(string); !ok || len(token) != 32 {
		t.Fatalf("expected 32-character BungeeGuard token, got %v", created["bungeeguard_allowed_token"])
	}
	oldToken := created["bungeeguard_allowed_token"].(string)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPost, "/api/v1/servers/1/bungeeguard-token", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate BungeeGuard token expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var rotated map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rotated)
	rotatedToken, _ := rotated["bungeeguard_allowed_token"].(string)
	if rotatedToken == oldToken || len(rotatedToken) != 32 {
		t.Fatalf("expected a new 32-character BungeeGuard token, got %v", rotated["bungeeguard_allowed_token"])
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPost, "/api/v1/rules/", map[string]any{
		"kind": "ip-deny", "target": "198.51.100.44", "server_id": 1,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server-scoped rule expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodDelete, "/api/v1/servers/1/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete server with related entities expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/servers/1/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("deleted server expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGeoPolicyEndpoints(t *testing.T) {
	r := newTestRouter(t)

	// global geo upsert
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPut, "/api/v1/policy/geo", map[string]any{
		"mode":      "deny",
		"countries": []string{"RU", "CN", "ru"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("put global geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// global geo get
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/policy/geo", nil))
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
	r.ServeHTTP(rec, authReq(t, http.MethodPost, "/api/v1/servers/", map[string]any{
		"name":     "survival",
		"upstream": "127.0.0.1:25567",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create server expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPut, "/api/v1/policy/geo?server_id=1", map[string]any{
		"mode":      "allow",
		"countries": []string{"KR"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("put server geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/policy/geo?server_id=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get server geo expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodDelete, "/api/v1/policy/geo?server_id=1", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset server geo expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/policy/geo?server_id=1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"disabled"`) {
		t.Fatalf("expected disabled geo policy after reset, got %d body=%s", rec.Code, rec.Body.String())
	}
}
