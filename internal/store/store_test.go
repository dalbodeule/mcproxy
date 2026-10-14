package store

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.minekube.com/common/minecraft/component"
	"mcproxy/internal/config"
	"mcproxy/internal/ent"
)

func TestMatchRuleIPSingle(t *testing.T) {
	r := &ent.Rule{Kind: "ip-deny", Target: "127.0.0.1"}
	if !matchRule(r, net.ParseIP("127.0.0.1"), "", "") {
		t.Fatalf("expected IP rule to match exact IP")
	}
	if matchRule(r, net.ParseIP("127.0.0.2"), "", "") {
		t.Fatalf("expected IP rule not to match other IP")
	}
}

func TestDistributedCounterFailClosedMode(t *testing.T) {
	closed := &Store{dist: &distributedCoordinator{}, counters: newBucketCounter(), distributedFailClosed: true}
	if _, err := closed.bumpCounter(context.Background(), nil, "ip", "198.51.100.1", 10, true); err == nil {
		t.Fatal("expected Redis counter failure to be returned in fail-closed mode")
	}
	if got := closed.DistributedMetrics(); !got.FailClosed || got.Errors != 1 || got.Fallbacks != 1 {
		t.Fatalf("unexpected fail-closed metrics: %+v", got)
	}

	open := &Store{dist: &distributedCoordinator{}, counters: newBucketCounter()}
	if count, err := open.bumpCounter(context.Background(), nil, "ip", "198.51.100.1", 10, true); err != nil || count != 1 {
		t.Fatalf("expected local fallback, count=%d err=%v", count, err)
	}
}

func TestAuditWriteFailureIsCounted(t *testing.T) {
	st := &Store{}
	if err := st.writeAudit(context.Background(), nil, nil, "test.audit", map[string]any{"ok": true}); err == nil {
		t.Fatal("expected audit persistence failure for an uninitialized store")
	}
	if got := st.AuditMetrics().WriteErrors; got != 1 {
		t.Fatalf("audit write errors = %d, want 1", got)
	}
}

func TestAuditWriteSanitizesMinecraftComponentBeforeEntJSON(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{
		DBDriver: "sqlite",
		DSN:      "file:" + filepath.Join(t.TempDir(), "audit-component.db") + "?_pragma=foreign_keys(1)",
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if err := st.writeAudit(ctx, nil, nil, "connect.disconnect", map[string]any{
		"reason": &component.Text{Content: "Unable to authenticate"},
	}); err != nil {
		t.Fatalf("write component audit: %v", err)
	}
	items, err := st.ListAudits(ctx, 1, 0, "connect.disconnect", nil)
	if err != nil {
		t.Fatalf("list component audit: %v", err)
	}
	if len(items) != 1 || items[0].Details["reason"] != "Unable to authenticate" {
		t.Fatalf("stored component audit = %#v, want sanitized reason", items)
	}
}

func TestAuditWriterRejectsEventsAfterStop(t *testing.T) {
	st := &Store{}
	w := newAuditWriter(st)
	w.stop()
	w.enqueue(auditEvent{event: "after.stop"})
	if got := st.AuditMetrics().WriteErrors; got != 1 {
		t.Fatalf("audit write errors after stopped enqueue = %d, want 1", got)
	}
	w.stop()
}

func TestMigrateRestoresNewServerColumns(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite",
		DSN:      "file:" + filepath.Join(t.TempDir(), "legacy.db") + "?_pragma=foreign_keys(1)",
	}
	ctx := context.Background()
	initial, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open initial store: %v", err)
	}
	if _, err := initial.CreateServer(ctx, ServerInput{Name: "legacy", Upstream: "127.0.0.1:25565"}); err != nil {
		_ = initial.Close()
		t.Fatalf("create legacy fixture: %v", err)
	}
	if err := initial.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	raw, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	for _, column := range []string{"forwarding_mode", "ping_motd", "bungeeguard_allowed_token"} {
		if _, err := raw.ExecContext(ctx, "ALTER TABLE servers DROP COLUMN "+column); err != nil {
			_ = raw.Close()
			t.Fatalf("remove legacy-missing column %s: %v", column, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := Migrate(ctx, cfg); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	st, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open migrated database: %v", err)
	}
	defer st.Close()
	servers, err := st.ListServers(ctx)
	if err != nil {
		t.Fatalf("list migrated servers: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "legacy" || len(servers[0].BungeeGuardAllowedToken) != 32 {
		t.Fatalf("unexpected migrated server: %+v", servers)
	}
}

func TestStoreCloseIsIdempotent(t *testing.T) {
	cfg := config.Config{
		DBDriver: "sqlite",
		DSN:      "file:" + filepath.Join(t.TempDir(), "close.db") + "?_pragma=foreign_keys(1)",
	}
	st, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestMatchRuleCIDR(t *testing.T) {
	r := &ent.Rule{Kind: "ip-allow", Target: "10.0.0.0/24", IsCidr: true}
	if !matchRule(r, net.ParseIP("10.0.0.25"), "", "") {
		t.Fatalf("expected CIDR rule to match IP in range")
	}
	if matchRule(r, net.ParseIP("10.0.1.25"), "", "") {
		t.Fatalf("expected CIDR rule not to match IP out of range")
	}
}

func TestMatchRuleNickname(t *testing.T) {
	r := &ent.Rule{Kind: "name-deny", Target: "Notch"}
	if !matchRule(r, nil, "notch", "") {
		t.Fatalf("expected nickname rule to be normalized and match")
	}
	if matchRule(r, nil, "jeb_", "") {
		t.Fatalf("expected nickname rule not to match other nickname")
	}
}

func TestMatchRuleASN(t *testing.T) {
	r := &ent.Rule{Kind: "asn-deny", Target: "AS64500"}
	if !matchRule(r, nil, "", "64500") {
		t.Fatal("expected ASN rule to normalize the numeric target")
	}
	if matchRule(r, nil, "", "AS64501") {
		t.Fatal("expected ASN rule not to match another ASN")
	}
	if normalizeASN("AS0") != "" || normalizeASN("not-an-asn") != "" {
		t.Fatal("invalid ASN values must normalize to empty")
	}
}

func TestMatchRulesPriorityBuckets(t *testing.T) {
	rules := []*ent.Rule{
		{ID: 1, Kind: "ip-allow", Target: "127.0.0.1"},
		{ID: 2, Kind: "name-deny", Target: "Steve"},
	}
	matched := matchRules(rules, net.ParseIP("127.0.0.1"), "steve", "")
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
	longReason := strings.Repeat("r", 513)
	if _, err := st.CreateRule(ctx, RuleInput{Kind: "ip-deny", Target: "203.0.113.8", IsCIDR: &ipDeny, Reason: &longReason}); err == nil {
		t.Fatal("expected oversized rule reason to be rejected")
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
	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("stats after decisions: %v", err)
	}
	if stats.Attempts != 4 || stats.Allowed != 1 || stats.DecisionBlocks != 3 {
		t.Fatalf("unexpected decision counters: attempts=%d allowed=%d blocks=%d", stats.Attempts, stats.Allowed, stats.DecisionBlocks)
	}
}

func TestEvaluateAttemptASNRule(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "asn.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateRule(ctx, RuleInput{Kind: "asn-deny", Target: "AS64500"}); err != nil {
		t.Fatalf("create ASN rule: %v", err)
	}
	result, err := st.EvaluateAttempt(ctx, EvaluateAttemptInput{IP: "198.51.100.50", Nickname: "asn", ASN: "64500"})
	if err != nil {
		t.Fatalf("evaluate ASN rule: %v", err)
	}
	if result.Allowed || result.Reason != "rule.deny" {
		t.Fatalf("expected ASN rule denial, got %#v", result)
	}
}

func TestEvaluateAttemptConcurrentInitialSnapshot(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "concurrent-snapshot.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Open preloads the snapshot; invalidate it to exercise the first
	// concurrent data-plane refresh rather than the warm-cache path.
	st.cache.invalidate()
	record := false
	const workers = 16
	results := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			result, evalErr := st.EvaluateAttempt(ctx, EvaluateAttemptInput{
				IP:       "198.51.100.80",
				Nickname: "snapshot-race",
				Record:   &record,
			})
			if evalErr != nil {
				results <- evalErr
				return
			}
			if result == nil || !result.Allowed {
				results <- fmt.Errorf("unexpected evaluation result: %#v", result)
				return
			}
			results <- nil
		}()
	}
	wg.Wait()
	close(results)
	for evalErr := range results {
		if evalErr != nil {
			t.Fatal(evalErr)
		}
	}
	metrics := st.CacheMetrics()
	if metrics.Misses == 0 || st.cache.stale() {
		t.Fatalf("unexpected snapshot state: misses=%d stale=%v", metrics.Misses, st.cache.stale())
	}
}

func TestEvaluationMetricsTrackServerAttempts(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, config.Config{DBDriver: "sqlite", DSN: "file:" + filepath.Join(t.TempDir(), "evaluation-metrics.db") + "?_pragma=foreign_keys(1)"})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	server, err := st.CreateServer(ctx, ServerInput{Name: "metrics", Upstream: "127.0.0.1:25565"})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	record := false
	for i := 0; i < 2; i++ {
		result, evalErr := st.EvaluateAttempt(ctx, EvaluateAttemptInput{ServerID: &server.ID, IP: "198.51.100.81", Nickname: "metrics", Record: &record})
		if evalErr != nil || result == nil || !result.Allowed {
			t.Fatalf("evaluate attempt %d: result=%#v err=%v", i, result, evalErr)
		}
	}
	metrics := st.EvaluationMetrics()
	if metrics.ServerAttempts[server.ID] != 2 || metrics.DurationCount != 2 {
		t.Fatalf("unexpected evaluation metrics: %+v", metrics)
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
	if !isValidBungeeGuardToken(server.BungeeGuardAllowedToken) {
		t.Fatalf("created token is not a 32-character lowercase hex token: %q", server.BungeeGuardAllowedToken)
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
	if !isValidBungeeGuardToken(rotated.BungeeGuardAllowedToken) || rotated.BungeeGuardAllowedToken == server.BungeeGuardAllowedToken {
		t.Fatalf("rotation did not produce a new 32-character lowercase hex token")
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
