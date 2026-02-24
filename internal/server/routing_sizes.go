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

type ipsetSnapshot struct {
	Count   int
	Members []string
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
	snapshots, err := readIPSetSnapshots(timeout)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(snapshots))
	for name, snapshot := range snapshots {
		result[name] = snapshot.Count
	}
	return result, nil
}

func readIPSetSnapshots(timeout time.Duration) (map[string]ipsetSnapshot, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ipset", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("ipset list failed: %w", err)
	}
	return parseIPSetSnapshots(string(out))
}

func parseIPSetSizes(raw string) (map[string]int, error) {
	snapshots, err := parseIPSetSnapshots(raw)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(snapshots))
	for name, snapshot := range snapshots {
		result[name] = snapshot.Count
	}
	return result, nil
}

func parseIPSetSnapshots(raw string) (map[string]ipsetSnapshot, error) {
	result := make(map[string]ipsetSnapshot)
	current := ""
	inMembers := false
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Name:") {
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
			inMembers = false
			if current != "" {
				if _, exists := result[current]; !exists {
					result[current] = ipsetSnapshot{}
				}
			}
			continue
		}
		if current == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "Number of entries:") {
			if strings.HasPrefix(trimmed, "Members:") {
				inMembers = true
				continue
			}
			if inMembers && trimmed != "" {
				member := parseIPSetMember(trimmed)
				if member != "" {
					snapshot := result[current]
					snapshot.Members = append(snapshot.Members, member)
					result[current] = snapshot
				}
			}
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "Number of entries:"))
		count, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid ipset count for %s: %w", current, err)
		}
		snapshot := result[current]
		snapshot.Count = count
		result[current] = snapshot
	}
	return result, nil
}

func parseIPSetMember(line string) string {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}
