package store

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"mcproxy/internal/ent"
)

// Pure policy/rule evaluation helpers are isolated from persistence and
// counter coordination so they remain cheap to test and safe to reuse.
func normalizeNickname(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizeCountryCodes(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, v := range in {
		code := strings.ToUpper(strings.TrimSpace(v))
		if len(code) != 2 {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func matchRules(rules []*ent.Rule, ip net.IP, nickname, asn string) map[string]string {
	matched := map[string]string{}
	for _, r := range rules {
		if matchRule(r, ip, nickname, asn) {
			switch r.Kind {
			case "ip-allow", "name-allow", "asn-allow":
				if _, ok := matched["allow"]; !ok {
					matched["allow"] = fmt.Sprintf("%d", r.ID)
				}
			case "ip-deny", "name-deny", "asn-deny":
				if _, ok := matched["deny"]; !ok {
					matched["deny"] = fmt.Sprintf("%d", r.ID)
				}
			}
		}
	}
	return matched
}

func matchRule(r *ent.Rule, ip net.IP, nickname, asn string) bool {
	target := strings.TrimSpace(r.Target)
	switch r.Kind {
	case "ip-allow", "ip-deny":
		if r.IsCidr {
			_, network, err := net.ParseCIDR(target)
			return err == nil && network.Contains(ip)
		}
		parsed := net.ParseIP(target)
		return parsed != nil && parsed.Equal(ip)
	case "name-allow", "name-deny":
		return normalizeNickname(target) == nickname
	case "asn-allow", "asn-deny":
		return normalizeASN(target) != "" && normalizeASN(target) == normalizeASN(asn)
	default:
		return false
	}
}

var asnPattern = regexp.MustCompile(`^(?:AS)?([0-9]{1,10})$`)

func normalizeASN(value string) string {
	match := asnPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(value)))
	if len(match) != 2 {
		return ""
	}
	digits := strings.TrimLeft(match[1], "0")
	if digits == "" {
		return ""
	}
	asnValue, err := strconv.ParseUint(digits, 10, 32)
	if err != nil || asnValue == 0 {
		return ""
	}
	return "AS" + strconv.FormatUint(asnValue, 10)
}

func evaluateThresholds(global *PolicyDTO, scoped *PolicyDTO, counts map[string]int) (bool, string) {
	if overThreshold(counts["ip_10s"], chooseMin(global.IPBurst10s, scopedValue(scoped, func(p *PolicyDTO) int { return p.IPBurst10s }))) {
		return true, "threshold.ip_10s"
	}
	if overThreshold(counts["ip_60s"], chooseMin(global.IPBurst60s, scopedValue(scoped, func(p *PolicyDTO) int { return p.IPBurst60s }))) {
		return true, "threshold.ip_60s"
	}
	if overThreshold(counts["name_10s"], chooseMin(global.NameBurst10s, scopedValue(scoped, func(p *PolicyDTO) int { return p.NameBurst10s }))) {
		return true, "threshold.name_10s"
	}
	if overThreshold(counts["name_60s"], chooseMin(global.NameBurst60s, scopedValue(scoped, func(p *PolicyDTO) int { return p.NameBurst60s }))) {
		return true, "threshold.name_60s"
	}
	return false, ""
}

func evaluateGeoPolicy(global *PolicyDTO, scoped *PolicyDTO, country string) (bool, string) {
	mode := global.GeoMode
	list := global.GeoList
	if scoped != nil && scoped.GeoMode != "" && scoped.GeoMode != "disabled" {
		mode = scoped.GeoMode
		list = scoped.GeoList
	}
	if mode == "" || mode == "disabled" {
		return false, ""
	}
	if country == "" {
		if mode == "allow" {
			return true, "geo.country_unknown"
		}
		return false, ""
	}
	matched := false
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), country) {
			matched = true
			break
		}
	}
	switch mode {
	case "allow":
		if !matched {
			return true, "geo.country_not_allowed"
		}
	case "deny":
		if matched {
			return true, "geo.country_denied"
		}
	}
	return false, ""
}

func chooseMin(global, scoped int) int {
	if scoped > 0 && (global == 0 || scoped < global) {
		return scoped
	}
	return global
}

func scopedValue(p *PolicyDTO, fn func(*PolicyDTO) int) int {
	if p == nil {
		return 0
	}
	return fn(p)
}

func overThreshold(current, limit int) bool {
	return limit > 0 && current > limit
}
