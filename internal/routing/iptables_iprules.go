package routing

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func (m *RuleManager) reconcileManagedIPRules(desired map[uint32]int, ipv6 bool) error {
	existing, loaded := m.loadManagedIPRules(ipv6)
	if !loaded {
		// Fallback path when we cannot inspect current state: keep old delete+add behavior.
		marks := sortedMarks(desired)
		for _, mark := range marks {
			if err := m.refreshIPRule(fmt.Sprintf("0x%x", mark), desired[mark], ipv6); err != nil {
				return err
			}
		}
		return nil
	}

	marks := sortedMarks(desired)
	for _, mark := range marks {
		table := desired[mark]
		key := ipRulePairKey(mark, table)
		if _, ok := existing[key]; ok {
			continue
		}
		args := []string{"rule", "add", "fwmark", fmt.Sprintf("0x%x", mark), "table", strconv.Itoa(table), "priority", rulePriority}
		if ipv6 {
			args = append([]string{"-6"}, args...)
		}
		if err := m.exec.Run("ip", args...); err != nil {
			family := "ipv4"
			if ipv6 {
				family = "ipv6"
			}
			return fmt.Errorf("add %s ip rule for mark 0x%x table %d: %w", family, mark, table, err)
		}
	}

	stale := make([]ipRulePair, 0, len(existing))
	for _, pair := range existing {
		if table, ok := desired[pair.Mark]; ok && table == pair.Table {
			continue
		}
		stale = append(stale, pair)
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].Mark == stale[j].Mark {
			return stale[i].Table < stale[j].Table
		}
		return stale[i].Mark < stale[j].Mark
	})
	for _, pair := range stale {
		deleteArgs := []string{
			"rule",
			"del",
			"fwmark",
			fmt.Sprintf("0x%x", pair.Mark),
			"table",
			strconv.Itoa(pair.Table),
			"priority",
			rulePriority,
		}
		if ipv6 {
			deleteArgs = append([]string{"-6"}, deleteArgs...)
		}
		for i := 0; i < deleteLoopLimit; i++ {
			if err := m.exec.Run("ip", deleteArgs...); err != nil {
				break
			}
		}
	}
	return nil
}

func sortedMarks(values map[uint32]int) []uint32 {
	marks := make([]uint32, 0, len(values))
	for mark := range values {
		marks = append(marks, mark)
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i] < marks[j] })
	return marks
}

type ipRulePair struct {
	Mark  uint32
	Table int
}

func ipRulePairKey(mark uint32, table int) string {
	return strconv.FormatUint(uint64(mark), 10) + ":" + strconv.Itoa(table)
}

func (m *RuleManager) loadManagedIPRules(ipv6 bool) (map[string]ipRulePair, bool) {
	args := []string{"rule", "show"}
	if ipv6 {
		args = append([]string{"-6"}, args...)
	}
	output, err := m.exec.Output("ip", args...)
	if err != nil {
		return nil, false
	}
	rules := make(map[string]ipRulePair)
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !strings.HasPrefix(trimmed, rulePriority+":") {
			continue
		}
		markToken, tableID, ok := parseIPRuleLine(trimmed)
		if !ok || tableID < 200 {
			continue
		}
		markValue, err := strconv.ParseUint(markToken, 0, 32)
		if err != nil || markValue < 200 {
			continue
		}
		pair := ipRulePair{Mark: uint32(markValue), Table: tableID}
		rules[ipRulePairKey(pair.Mark, pair.Table)] = pair
	}
	return rules, true
}

func (m *RuleManager) refreshIPRule(markHex string, routeTable int, ipv6 bool) error {
	argsDelete := []string{"rule", "del", "fwmark", markHex, "table", strconv.Itoa(routeTable), "priority", rulePriority}
	argsAdd := []string{"rule", "add", "fwmark", markHex, "table", strconv.Itoa(routeTable), "priority", rulePriority}
	if ipv6 {
		argsDelete = append([]string{"-6"}, argsDelete...)
		argsAdd = append([]string{"-6"}, argsAdd...)
	}
	for i := 0; i < deleteLoopLimit; i++ {
		if err := m.exec.Run("ip", argsDelete...); err != nil {
			break
		}
	}
	if err := m.exec.Run("ip", argsAdd...); err != nil {
		family := "ipv4"
		if ipv6 {
			family = "ipv6"
		}
		return fmt.Errorf("add %s ip rule for mark %s table %d: %w", family, markHex, routeTable, err)
	}
	return nil
}

func (m *RuleManager) flushManagedIPRules(ipv6 bool) error {
	existing, loaded := m.loadManagedIPRules(ipv6)
	if !loaded {
		return nil
	}
	stale := make([]ipRulePair, 0, len(existing))
	for _, pair := range existing {
		stale = append(stale, pair)
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].Mark == stale[j].Mark {
			return stale[i].Table < stale[j].Table
		}
		return stale[i].Mark < stale[j].Mark
	})
	for _, pair := range stale {
		deleteArgs := []string{
			"rule",
			"del",
			"fwmark",
			fmt.Sprintf("0x%x", pair.Mark),
			"table",
			strconv.Itoa(pair.Table),
			"priority",
			rulePriority,
		}
		if ipv6 {
			deleteArgs = append([]string{"-6"}, deleteArgs...)
		}
		for i := 0; i < deleteLoopLimit; i++ {
			if delErr := m.exec.Run("ip", deleteArgs...); delErr != nil {
				break
			}
		}
	}
	return nil
}

func parseIPRuleLine(line string) (string, int, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 6 {
		return "", 0, false
	}
	mark := ""
	table := -1
	for i := 0; i < len(fields)-1; i++ {
		switch fields[i] {
		case "fwmark":
			mark = strings.Split(strings.TrimSpace(fields[i+1]), "/")[0]
		case "lookup", "table":
			if n, err := strconv.Atoi(fields[i+1]); err == nil {
				table = n
			}
		}
	}
	if mark == "" || table < 0 {
		return "", 0, false
	}
	return mark, table, true
}
