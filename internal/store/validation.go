package store

import (
	"fmt"
	"net"
	"strings"
)

// Validation for control-plane inputs lives outside store.go so data access,
// policy evaluation, and request-boundary rules can evolve independently.
func validateServerInput(in ServerInput, create bool) error {
	if create && strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if create && strings.TrimSpace(in.Upstream) == "" {
		return fmt.Errorf("upstream is required")
	}
	if strings.TrimSpace(in.Name) != "" && len([]rune(strings.TrimSpace(in.Name))) > 128 {
		return fmt.Errorf("name is too long")
	}
	if strings.TrimSpace(in.Upstream) != "" {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(in.Upstream)); err != nil {
			return fmt.Errorf("upstream must be host:port")
		}
	}
	if in.Priority != nil && (*in.Priority < 0 || *in.Priority > 1_000_000) {
		return fmt.Errorf("priority must be between 0 and 1000000")
	}
	if in.PublicHost != nil && strings.TrimSpace(*in.PublicHost) != "" {
		host := strings.TrimSpace(*in.PublicHost)
		if strings.ContainsAny(host, "/?#@") || net.ParseIP(host) != nil {
			return fmt.Errorf("invalid public_host")
		}
	}
	if in.PublicAddr != nil && strings.TrimSpace(*in.PublicAddr) != "" {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(*in.PublicAddr)); err != nil {
			return fmt.Errorf("public_addr must be host:port")
		}
	}
	if in.PingMOTD != nil && len([]rune(strings.TrimSpace(*in.PingMOTD))) > 256 {
		return fmt.Errorf("ping_motd is too long")
	}
	if in.ForwardingMode != nil && !isValidForwardingMode(*in.ForwardingMode) {
		return fmt.Errorf("forwarding_mode must be one of none, legacy, velocity, or bungeeguard")
	}
	return nil
}

func validateRuleInput(in RuleInput) error {
	switch strings.TrimSpace(in.Kind) {
	case "ip-allow", "ip-deny", "name-allow", "name-deny", "asn-allow", "asn-deny":
	default:
		return fmt.Errorf("invalid kind")
	}
	if strings.TrimSpace(in.Target) == "" {
		return fmt.Errorf("target is required")
	}
	if len([]rune(strings.TrimSpace(in.Target))) > 256 {
		return fmt.Errorf("target is too long")
	}
	if in.Reason != nil && len([]rune(strings.TrimSpace(*in.Reason))) > 512 {
		return fmt.Errorf("reason is too long")
	}
	if strings.HasPrefix(in.Kind, "ip-") {
		if in.IsCIDR != nil && *in.IsCIDR {
			if _, _, err := net.ParseCIDR(strings.TrimSpace(in.Target)); err != nil {
				return fmt.Errorf("invalid cidr target")
			}
		} else if net.ParseIP(strings.TrimSpace(in.Target)) == nil {
			return fmt.Errorf("invalid ip target")
		}
	}
	if strings.HasPrefix(in.Kind, "asn-") {
		if in.IsCIDR != nil && *in.IsCIDR {
			return fmt.Errorf("ASN targets cannot be CIDR")
		}
		if normalizeASN(in.Target) == "" {
			return fmt.Errorf("invalid ASN target")
		}
	}
	return nil
}

func validatePolicyInput(in PolicyInput) error {
	for name, value := range map[string]*int{
		"ip_burst_10s":   in.IPBurst10s,
		"ip_burst_60s":   in.IPBurst60s,
		"name_burst_10s": in.NameBurst10s,
		"name_burst_60s": in.NameBurst60s,
	} {
		if value != nil && (*value < 0 || *value > 1_000_000) {
			return fmt.Errorf("%s must be between 0 and 1000000", name)
		}
	}
	if in.KickMessage != nil && len([]rune(*in.KickMessage)) > 512 {
		return fmt.Errorf("kick_message is too long")
	}
	if in.GeoMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*in.GeoMode))
		if mode != "disabled" && mode != "allow" && mode != "deny" {
			return fmt.Errorf("invalid geo mode")
		}
	}
	if in.GeoList != nil && len(in.GeoList) > 256 {
		return fmt.Errorf("geo_list is too large")
	}
	return nil
}
