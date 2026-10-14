package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcproxy/internal/testutil"
)

func TestAdminActorContextFallsBackAndPreservesPanelIdentity(t *testing.T) {
	if got := adminActor(context.Background()); got != "admin-api" {
		t.Fatalf("default admin actor = %q, want admin-api", got)
	}
	ctx := context.WithValue(context.Background(), adminActorContextKey{}, "operator@example.test")
	if got := adminActor(ctx); got != "operator@example.test" {
		t.Fatalf("panel admin actor = %q, want operator@example.test", got)
	}
}

func TestVerifiedAdminActorRequiresPanelSignature(t *testing.T) {
	if got := verifiedAdminActor("panel-secret", "operator@example.test", ""); got != "" {
		t.Fatalf("unsigned actor was trusted: %q", got)
	}
	digest := hmac.New(sha256.New, []byte("panel-secret"))
	_, _ = digest.Write([]byte("operator@example.test"))
	signature := hex.EncodeToString(digest.Sum(nil))
	if got := verifiedAdminActor("panel-secret", "operator@example.test", signature); got != "operator@example.test" {
		t.Fatalf("signed actor = %q", got)
	}
	if got := verifiedAdminActor("panel-secret", "operator@example.test", signature[:len(signature)-2]+"00"); got != "" {
		t.Fatalf("tampered actor signature was trusted: %q", got)
	}
}

func TestInvalidAdminActorSignatureMetric(t *testing.T) {
	invalidAdminActorSignatures.Store(0)
	cfg := testutil.NewTestConfig(t)
	cfg.AdminActorSecret = "panel-audit-secret-012345678901234567890"
	r := NewRouter(cfg, Dependencies{Store: testutil.NewTestStore(t)})
	req := authReq(t, http.MethodGet, "/api/v1/audits?limit=1", nil)
	req.Header.Set("X-Admin-Actor", "operator@example.test")
	req.Header.Set("X-Admin-Actor-Signature", "00")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid actor request should remain authenticated, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "mcproxy_admin_actor_signature_invalid_total 1") {
		t.Fatalf("invalid actor metric was not incremented: %s", rec.Body.String())
	}
}

func TestPromEscapeKeepsLabelValueOnOneLine(t *testing.T) {
	got := promEscape("reason\\\"\n\r")
	if got != `reason\\\"\n\r` {
		t.Fatalf("promEscape = %q", got)
	}
}

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
	var auditPage map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &auditPage); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if _, ok := auditPage["has_more"]; !ok {
		t.Fatalf("audit page omitted has_more: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "mcproxy_build_info{") || !strings.Contains(rec.Body.String(), `version="dev"`) {
		t.Fatalf("build metadata metric missing: %s", rec.Body.String())
	}
}

func TestCORSPreflightAllowsSignedAdminActorHeaders(t *testing.T) {
	cfg := testutil.NewTestConfig(t)
	cfg.AdminAllowedOrigins = []string{"https://panel.example.test"}
	r := NewRouter(cfg, Dependencies{Store: testutil.NewTestStore(t)})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/stats", nil)
	req.Header.Set("Origin", "https://panel.example.test")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("CORS preflight status = %d, want 204", rec.Code)
	}
	want := "Authorization, Content-Type, X-API-Token, X-Admin-Actor, X-Admin-Actor-Signature"
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != want {
		t.Fatalf("CORS allowed headers = %q, want %q", got, want)
	}
}

func TestAuditPaginationReportsLookahead(t *testing.T) {
	st := testutil.NewTestStore(t)
	const event = "test.audit.pagination"
	for i := 0; i < 3; i++ {
		if err := st.RecordAudit(context.Background(), "pagination-test", nil, event, map[string]any{"index": i}); err != nil {
			t.Fatalf("record audit %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		items, err := st.ListAudits(context.Background(), 10, 0, event, nil)
		if err != nil {
			t.Fatalf("list seeded audits: %v", err)
		}
		if len(items) == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for audit writer, got %d items", len(items))
		}
		time.Sleep(10 * time.Millisecond)
	}

	r := NewRouter(testutil.NewTestConfig(t), Dependencies{Store: st})
	req := authReq(t, http.MethodGet, "/api/v1/audits?event="+event+"&limit=2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit page expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items   []json.RawMessage `json:"items"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode audit page: %v", err)
	}
	if len(page.Items) != 2 || !page.HasMore {
		t.Fatalf("audit page = (%d items, has_more=%v), want (2, true): %s", len(page.Items), page.HasMore, rec.Body.String())
	}
}

func TestAuditServerFilterBounds(t *testing.T) {
	r := newTestRouter(t)
	values := strings.TrimSuffix(strings.Repeat("1,", 257), ",")
	req := authReq(t, http.MethodGet, "/api/v1/audits?server_id="+values, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "too many values") {
		t.Fatalf("expected bounded server_id filter, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuditOffsetIsBounded(t *testing.T) {
	limit, offset := queryPage(httptest.NewRequest(http.MethodGet, "/api/v1/audits?limit=20&offset=999999999", nil))
	if limit != 20 || offset != maxAuditOffset {
		t.Fatalf("audit page = (%d, %d), want (20, %d)", limit, offset, maxAuditOffset)
	}
}

func TestJSONBodyLimit(t *testing.T) {
	r := newTestRouter(t)
	body := strings.Repeat(" ", int(maxJSONBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/servers/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("expected oversized JSON body rejection, got %d body=%s", rec.Code, rec.Body.String())
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
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mcproxy_admin_actor_signature_invalid_total") || !strings.Contains(rec.Body.String(), "mcproxy_audit_write_errors_total 0") || !strings.Contains(rec.Body.String(), "mcproxy_gate_ready 1") || !strings.Contains(rec.Body.String(), "mcproxy_gate_players 4") || !strings.Contains(rec.Body.String(), "mcproxy_gate_backends 2") || !strings.Contains(rec.Body.String(), "mcproxy_snapshot_cache_age_seconds") || !strings.Contains(rec.Body.String(), "mcproxy_evaluation_latency_seconds_count 0") || !strings.Contains(rec.Body.String(), "mcproxy_snapshot_refreshes_total") || !strings.Contains(rec.Body.String(), "mcproxy_store_stats_latency_seconds_count") || !strings.Contains(rec.Body.String(), "mcproxy_db_open_connections") || !strings.Contains(rec.Body.String(), "mcproxy_db_wait_duration_seconds_total") || !strings.Contains(rec.Body.String(), "mcproxy_distributed_enabled 0") || !strings.Contains(rec.Body.String(), "mcproxy_distributed_healthy 1") || !strings.Contains(rec.Body.String(), "mcproxy_distributed_fail_closed 0") || !strings.Contains(rec.Body.String(), "mcproxy_distributed_fallbacks_total 0") {
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

func TestAdminAuthTrustedProxyHeadersUseClientIP(t *testing.T) {
	cfg := testutil.NewTestConfig(t)
	cfg.TrustProxyHeaders = true
	cfg.TrustedProxyCIDRs = []string{"127.0.0.0/8"}
	st := testutil.NewTestStore(t)
	r := NewRouter(cfg, Dependencies{Store: st})

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.RemoteAddr = "127.0.0.1:4000"
		req.Header.Set("Authorization", "Bearer incorrect-token")
		req.Header.Set("X-Forwarded-For", "198.51.100.10")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d expected 401, got %d", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("Authorization", "Bearer incorrect-token")
	req.Header.Set("X-Forwarded-For", "198.51.100.10")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("21st failure for forwarded client expected 429, got %d", rec.Code)
	}
}

func TestAdminAuthIgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	cfg := testutil.NewTestConfig(t)
	cfg.TrustProxyHeaders = true
	cfg.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	st := testutil.NewTestStore(t)
	r := NewRouter(cfg, Dependencies{Store: st})

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.RemoteAddr = "127.0.0.1:4000"
		req.Header.Set("Authorization", "Bearer incorrect-token")
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d expected 401, got %d", i+1, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.RemoteAddr = "127.0.0.1:4000"
	req.Header.Set("Authorization", "Bearer incorrect-token")
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted forwarded headers must not bypass limit, got %d", rec.Code)
	}
}

func TestServerLifecycleAndServerPolicy(t *testing.T) {
	r := newTestRouter(t)

	// BungeeGuard tokens are generated by the store and can only be rotated
	// through the dedicated endpoint; a normal server payload must not inject
	// an operator-supplied token.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodPost, "/api/v1/servers/", map[string]any{
		"name":                      "token-injection",
		"upstream":                  "127.0.0.1:25566",
		"bungeeguard_allowed_token": "0123456789abcdef0123456789abcdef",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("server token injection expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	// create server
	rec = httptest.NewRecorder()
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
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/audits?server_id=1", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"event":"server.create"`) {
		t.Fatalf("server-scoped audit did not include server.create: %d %s", rec.Code, rec.Body.String())
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
	var createdRule map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createdRule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	ruleID := int(createdRule["id"].(float64))
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, authReq(t, http.MethodDelete, fmt.Sprintf("/api/v1/rules/%d", ruleID), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete server-scoped rule expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	var auditBody string
	for attempt := 0; attempt < 20; attempt++ {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, authReq(t, http.MethodGet, "/api/v1/audits?server_id=1", nil))
		auditBody = rec.Body.String()
		if rec.Code == http.StatusOK && strings.Contains(auditBody, `"event":"rule.delete"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rec.Code != http.StatusOK || !strings.Contains(auditBody, `"event":"rule.delete"`) {
		t.Fatalf("server-scoped rule deletion audit missing server scope: %d %s", rec.Code, auditBody)
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
