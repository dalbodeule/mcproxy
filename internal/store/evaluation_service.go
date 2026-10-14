package store

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// EvaluateAttempt is the data-plane orchestration path. Pure matching and
// threshold/Geo decisions live in evaluation.go; cache and counter adapters
// are kept behind Store methods so callers do not access persistence directly.
func (s *Store) EvaluateAttempt(ctx context.Context, in EvaluateAttemptInput) (*EvaluateAttemptResult, error) {
	started := time.Now()
	rawIP := strings.TrimSpace(in.IP)
	if len(rawIP) > 128 {
		return nil, fmt.Errorf("ip is too long")
	}
	if len([]rune(strings.TrimSpace(in.Nickname))) > 128 {
		return nil, fmt.Errorf("nickname is too long")
	}
	if len([]rune(strings.TrimSpace(in.Country))) > 16 {
		return nil, fmt.Errorf("country is too long")
	}
	if len([]rune(strings.TrimSpace(in.ASN))) > 16 {
		return nil, fmt.Errorf("asn is too long")
	}
	ip := net.ParseIP(rawIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip")
	}
	s.attempts.Add(1)
	defer func() {
		s.evaluationDurationNS.Add(time.Since(started).Nanoseconds())
		s.evaluationDurationCount.Add(1)
		s.recordServerAttempt(in.ServerID)
	}()
	name := normalizeNickname(in.Nickname)
	record := true
	if in.Record != nil {
		record = *in.Record
	}

	globalPolicy, err := s.cachedGlobalPolicy(ctx)
	if err != nil {
		return nil, err
	}
	serverPolicy, err := s.cachedServerPolicy(ctx, in.ServerID)
	if err != nil {
		return nil, err
	}
	relevantRules, err := s.cachedRules(ctx, in.ServerID)
	if err != nil {
		return nil, err
	}

	asn := normalizeASN(in.ASN)
	matched := matchRules(relevantRules, ip, name, asn)
	denyFirst := globalPolicy.DenyFirst
	if serverPolicy != nil {
		denyFirst = serverPolicy.DenyFirst
	}

	if denyFirst {
		if id, ok := matched["deny"]; ok {
			s.decisionBlocks.Add(1)
			_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": "rule.deny", "rule_id": id, "ip": in.IP, "nickname": in.Nickname, "asn": asn})
			return &EvaluateAttemptResult{Allowed: false, Reason: "rule.deny", Matched: map[string]string{"rule_id": id}}, nil
		}
		if id, ok := matched["allow"]; ok {
			s.allowed.Add(1)
			return &EvaluateAttemptResult{Allowed: true, Reason: "rule.allow", Matched: map[string]string{"rule_id": id}}, nil
		}
	} else {
		if id, ok := matched["allow"]; ok {
			s.allowed.Add(1)
			return &EvaluateAttemptResult{Allowed: true, Reason: "rule.allow", Matched: map[string]string{"rule_id": id}}, nil
		}
		if id, ok := matched["deny"]; ok {
			s.decisionBlocks.Add(1)
			_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": "rule.deny", "rule_id": id, "ip": in.IP, "nickname": in.Nickname, "asn": asn})
			return &EvaluateAttemptResult{Allowed: false, Reason: "rule.deny", Matched: map[string]string{"rule_id": id}}, nil
		}
	}

	counts := map[string]int{}
	if in.IP != "" {
		c10, err := s.bumpCounter(ctx, in.ServerID, "ip", in.IP, 10, record)
		if err != nil {
			return nil, err
		}
		c60, err := s.bumpCounter(ctx, in.ServerID, "ip", in.IP, 60, record)
		if err != nil {
			return nil, err
		}
		counts["ip_10s"] = c10
		counts["ip_60s"] = c60
	}
	if name != "" {
		c10, err := s.bumpCounter(ctx, in.ServerID, "name", name, 10, record)
		if err != nil {
			return nil, err
		}
		c60, err := s.bumpCounter(ctx, in.ServerID, "name", name, 60, record)
		if err != nil {
			return nil, err
		}
		counts["name_10s"] = c10
		counts["name_60s"] = c60
	}

	if exceeded, reason := evaluateThresholds(globalPolicy, serverPolicy, counts); exceeded {
		s.decisionBlocks.Add(1)
		_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": reason, "ip": in.IP, "nickname": in.Nickname, "counts": counts})
		return &EvaluateAttemptResult{Allowed: false, Reason: reason, Counts: counts}, nil
	}

	country := strings.ToUpper(strings.TrimSpace(in.Country))
	if blocked, reason := evaluateGeoPolicy(globalPolicy, serverPolicy, country); blocked {
		s.decisionBlocks.Add(1)
		_ = s.recordAudit(ctx, in.ServerID, "connect.blocked", map[string]any{"reason": reason, "ip": in.IP, "nickname": in.Nickname, "country": country})
		return &EvaluateAttemptResult{Allowed: false, Reason: reason, Counts: counts, Matched: map[string]string{"country": country}}, nil
	}

	s.allowed.Add(1)
	return &EvaluateAttemptResult{Allowed: true, Reason: "allowed", Counts: counts}, nil
}
