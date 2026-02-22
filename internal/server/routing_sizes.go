package server

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"split-vpn-webui/internal/routing"
)

type vpnRoutingSizes struct {
	V4 int
	V6 int
}

func (s *Server) collectRoutingSizes(ctx context.Context) (map[string]vpnRoutingSizes, error) {
	if s.routingManager == nil {
		return map[string]vpnRoutingSizes{}, nil
	}
	groups, err := s.routingManager.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return map[string]vpnRoutingSizes{}, nil
	}

	allSetSizes, err := readIPSetSizes(5 * time.Second)
	if err != nil {
		return nil, err
	}

	out := make(map[string]vpnRoutingSizes)
	for _, group := range groups {
		vpnName := strings.TrimSpace(group.EgressVPN)
		if vpnName == "" {
			continue
		}
		current := out[vpnName]
		for ruleIndex, rule := range group.Rules {
			sets := routing.RuleSetNames(group.Name, ruleIndex)
			if ruleNeedsSourceSet(rule) {
				current.V4 += allSetSizes[sets.SourceV4]
				current.V6 += allSetSizes[sets.SourceV6]
			}
			if ruleNeedsDestinationSet(rule) {
				current.V4 += allSetSizes[sets.DestinationV4]
				current.V6 += allSetSizes[sets.DestinationV6]
			}
		}
		out[vpnName] = current
	}
	return out, nil
}

func ruleNeedsSourceSet(rule routing.RoutingRule) bool {
	return len(rule.SourceCIDRs) > 0
}

func ruleNeedsDestinationSet(rule routing.RoutingRule) bool {
	return len(rule.DestinationCIDRs) > 0 ||
		len(rule.DestinationASNs) > 0 ||
		len(rule.Domains) > 0 ||
		len(rule.WildcardDomains) > 0
}

func readIPSetSizes(timeout time.Duration) (map[string]int, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ipset", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("ipset list failed: %w", err)
	}
	return parseIPSetSizes(string(out))
}

func parseIPSetSizes(raw string) (map[string]int, error) {
	result := make(map[string]int)
	current := ""
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Name:") {
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
			continue
		}
		if !strings.HasPrefix(trimmed, "Number of entries:") {
			continue
		}
		if current == "" {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "Number of entries:"))
		count, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid ipset count for %s: %w", current, err)
		}
		result[current] = count
	}
	return result, nil
}
