package store

import (
	"net"
	"testing"

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
