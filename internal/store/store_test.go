package store

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcproxy/internal/config"
	"mcproxy/internal/ent"
)

func TestMatchRuleIPSingle(t *testing.T) {
	r := &ent.Rule{Kind: "ip-deny", Target: "127.0.0.1"}
	if !matchRule(r, net.ParseIP("127.0.0.1"), "") {
		t.Fatalf("expected IP rule to match exact IP")
	}
	if matchRule(r, net.ParseIP("127.0.0.2"), "") {
		t.Fatalf("expected IP rule not to match other IP")
	}
}

func TestMatchRuleCIDR(t *testing.T) {
	r := &ent.Rule{Kind: "ip-allow", Target: "10.0.0.0/24", IsCidr: true}
	if !matchRule(r, net.ParseIP("10.0.0.25"), "") {
		t.Fatalf("expected CIDR rule to match IP in range")
	}
	if matchRule(r, net.ParseIP("10.0.1.25"), "") {
		t.Fatalf("expected CIDR rule not to match IP out of range")
	}
}

func TestMatchRuleNickname(t *testing.T) {
	r := &ent.Rule{Kind: "name-deny", Target: "Notch"}
	if !matchRule(r, nil, "notch") {
		t.Fatalf("expected nickname rule to be normalized and match")
	}
	if matchRule(r, nil, "jeb_") {
		t.Fatalf("expected nickname rule not to match other nickname")
	}
}

func TestMatchRulesPriorityBuckets(t *testing.T) {
	rules := []*ent.Rule{
		{ID: 1, Kind: "ip-allow", Target: "127.0.0.1"},
		{ID: 2, Kind: "name-deny", Target: "Steve"},
	}
	matched := matchRules(rules, net.ParseIP("127.0.0.1"), "steve")
	if matched["allow"] != "1" {
		t.Fatalf("expected allow match to be rule 1, got %q", matched["allow"])
	}
	if matched["deny"] != "2" {
		t.Fatalf("expected deny match to be rule 2, got %q", matched["deny"])
	}
}

func TestEvaluateThresholds(t *testing.T) {
	global := &PolicyDTO{IPBurst10s: 20, IPBurst60s: 80, NameBurst10s: 10, NameBurst60s: 40}
	server := &PolicyDTO{IPBurst10s: 5, NameBurst60s: 15}

	blocked, reason := evaluateThresholds(global, server, map[string]int{
		"ip_10s":   6,
		"ip_60s":   1,
		"name_10s": 1,
		"name_60s": 1,
	})
	if !blocked || reason != "threshold.ip_10s" {
		t.Fatalf("expected ip_10s threshold block, got blocked=%v reason=%q", blocked, reason)
	}

	blocked, reason = evaluateThresholds(global, server, map[string]int{
		"ip_10s":   1,
		"ip_60s":   1,
		"name_10s": 1,
		"name_60s": 16,
	})
	if !blocked || reason != "threshold.name_60s" {
		t.Fatalf("expected name_60s threshold block, got blocked=%v reason=%q", blocked, reason)
	}
}

func TestEvaluateGeoPolicy(t *testing.T) {
	global := &PolicyDTO{GeoMode: "allow", GeoList: []string{"KR", "JP"}}
	blocked, reason := evaluateGeoPolicy(global, nil, "US")
	if !blocked || reason != "geo.country_not_allowed" {
		t.Fatalf("expected geo allowlist block, got blocked=%v reason=%q", blocked, reason)
	}

	global = &PolicyDTO{GeoMode: "deny", GeoList: []string{"RU"}}
	blocked, reason = evaluateGeoPolicy(global, nil, "RU")
	if !blocked || reason != "geo.country_denied" {
		t.Fatalf("expected geo denylist block, got blocked=%v reason=%q", blocked, reason)
	}

	server := &PolicyDTO{GeoMode: "allow", GeoList: []string{"US"}}
	blocked, reason = evaluateGeoPolicy(global, server, "US")
	if blocked {
		t.Fatalf("expected server-scoped geo override to allow, got reason=%q", reason)
	}
}

func TestNormalizeCountryCodes(t *testing.T) {
	got := normalizeCountryCodes([]string{" kr ", "KR", "jp", "XXX", ""})
	if len(got) != 2 || got[0] != "KR" || got[1] != "JP" {
		t.Fatalf("unexpected normalized countries: %#v", got)
	}
}

func TestEvaluateAttemptIntegration(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "evaluate.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ipDeny := false
	doNotRecord := false
	if _, err := st.CreateRule(ctx, RuleInput{Kind: "ip-deny", Target: "203.0.113.7", IsCIDR: &ipDeny}); err != nil {
		t.Fatalf("create deny rule: %v", err)
	}
	denied, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "203.0.113.7", Nickname: "blocked", Record: &doNotRecord})
	if err != nil {
		t.Fatalf("evaluate deny rule: %v", err)
	}
	if denied.Allowed || denied.Reason != "rule.deny" {
		t.Fatalf("expected rule denial, got %#v", denied)
	}

	limit := 1
	if _, err := st.UpsertGlobalPolicy(ctx, PolicyInput{IPBurst10s: &limit}); err != nil {
		t.Fatalf("upsert threshold policy: %v", err)
	}
	first, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "198.51.100.20", Nickname: "first"})
	if err != nil || !first.Allowed {
		t.Fatalf("expected first attempt allowed, result=%#v err=%v", first, err)
	}
	second, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "198.51.100.20", Nickname: "second"})
	if err != nil {
		t.Fatalf("evaluate threshold: %v", err)
	}
	if second.Allowed || second.Reason != "threshold.ip_10s" {
		t.Fatalf("expected threshold denial, got %#v", second)
	}

	geoMode := "allow"
	if _, err := st.UpsertGlobalPolicy(ctx, PolicyInput{GeoMode: &geoMode, GeoList: []string{"KR"}}); err != nil {
		t.Fatalf("upsert geo policy: %v", err)
	}
	geoDenied, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "198.51.100.21", Nickname: "geo", Country: "US", Record: &doNotRecord})
	if err != nil {
		t.Fatalf("evaluate geo policy: %v", err)
	}
	if geoDenied.Allowed || geoDenied.Reason != "geo.country_not_allowed" {
		t.Fatalf("expected geo denial, got %#v", geoDenied)
	}
}

func TestServerBungeeGuardTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "tokens.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	mode := "bungeeguard"
	server, err := st.CreateServer(ctx, ServerInput{Name: "token-test", Upstream: "127.0.0.1:25565", ForwardingMode: &mode})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if len(server.BungeeGuardAllowedToken) != 32 {
		t.Fatalf("token length = %d, want 32", len(server.BungeeGuardAllowedToken))
	}
	if server.ForwardingMode != "bungeeguard" {
		t.Fatalf("forwarding mode = %q, want bungeeguard", server.ForwardingMode)
	}
	if _, err := st.client.Server.UpdateOneID(server.ID).SetBungeeguardAllowedToken("my-secret-token").Save(ctx); err != nil {
		t.Fatalf("seed malformed token: %v", err)
	}
	repaired, err := st.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("repair malformed token: %v", err)
	}
	if !isValidBungeeGuardToken(repaired.BungeeGuardAllowedToken) || repaired.BungeeGuardAllowedToken == "my-secret-token" {
		t.Fatalf("malformed token was not replaced: %q", repaired.BungeeGuardAllowedToken)
	}
	server = repaired

	updated, err := st.UpdateServer(ctx, server.ID, ServerInput{Name: "token-test", Upstream: "127.0.0.1:25566"})
	if err != nil {
		t.Fatalf("update server: %v", err)
	}
	if updated.BungeeGuardAllowedToken != server.BungeeGuardAllowedToken {
		t.Fatalf("ordinary update unexpectedly rotated token")
	}

	rotated, err := st.RotateBungeeGuardToken(ctx, server.ID)
	if err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	if len(rotated.BungeeGuardAllowedToken) != 32 || rotated.BungeeGuardAllowedToken == server.BungeeGuardAllowedToken {
		t.Fatalf("rotation did not produce a new 32-character token")
	}
}

func TestStorePublishesControlPlaneInvalidation(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "invalidations.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	server, err := st.CreateServer(ctx, ServerInput{Name: "invalidate-test", Upstream: "127.0.0.1:25565"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	select {
	case <-st.Invalidations():
	case <-time.After(time.Second):
		t.Fatal("server create did not publish an invalidation")
	}
	enabled := false
	if _, err := st.UpdateServer(ctx, server.ID, ServerInput{Enabled: &enabled}); err != nil {
		t.Fatalf("update server: %v", err)
	}
	select {
	case <-st.Invalidations():
	case <-time.After(time.Second):
		t.Fatal("server update did not publish an invalidation")
	}
}

func TestEvaluateAttemptRejectsOversizedInput(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "oversized.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "203.0.113.1", Nickname: strings.Repeat("n", 129)}); err == nil {
		t.Fatal("expected oversized nickname to be rejected")
	}
	if _, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: strings.Repeat("1", 129), Nickname: "player"}); err == nil {
		t.Fatal("expected oversized IP input to be rejected")
	}
}
