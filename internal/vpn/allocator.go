package vpn

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	minRouteTableID = 200
	minFWMark       = 200
	maxRouteTableID = 65535
	maxFWMark       = 0x7fffffff
)

var (
	// ErrAllocationConflict indicates a requested table/mark is already in use.
	ErrAllocationConflict = errors.New("allocation conflict")
	// ErrAllocationExhausted indicates no free table/mark value is available.
	ErrAllocationExhausted = errors.New("allocation exhausted")
)

// CommandExecutor abstracts command execution for allocator tests.
type CommandExecutor interface {
	CombinedOutput(name string, args ...string) ([]byte, error)
}

type systemCommandExecutor struct{}

func (systemCommandExecutor) CombinedOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Allocator coordinates route table and fwmark allocation.
type Allocator struct {
	mu sync.Mutex

	vpnsDir         string
	routeTablesPath string
	configRoots     []string
	exec            CommandExecutor

	usedTables   map[int]struct{}
	usedMarks    map[uint32]struct{}
	stickyTables map[int]struct{}
	stickyMarks  map[uint32]struct{}
}

// NewAllocator creates an allocator using live system information.
func NewAllocator(vpnsDir string) (*Allocator, error) {
	return newAllocator(vpnsDir, "/etc/iproute2/rt_tables", systemCommandExecutor{}, nil)
}

// NewAllocatorWithConfigRoots creates an allocator that additionally scans
// external config roots (e.g. peacey /data/split-vpn) for persisted allocations.
func NewAllocatorWithConfigRoots(vpnsDir string, configRoots []string) (*Allocator, error) {
	return newAllocator(vpnsDir, "/etc/iproute2/rt_tables", systemCommandExecutor{}, configRoots)
}

// NewAllocatorWithDeps creates an allocator with custom dependencies for tests.
func NewAllocatorWithDeps(vpnsDir, routeTablesPath string, executor CommandExecutor) (*Allocator, error) {
	return newAllocator(vpnsDir, routeTablesPath, executor, nil)
}

// NewAllocatorWithDepsAndConfigRoots creates an allocator with custom
// dependencies and additional config roots for tests.
func NewAllocatorWithDepsAndConfigRoots(
	vpnsDir, routeTablesPath string,
	executor CommandExecutor,
	configRoots []string,
) (*Allocator, error) {
	return newAllocator(vpnsDir, routeTablesPath, executor, configRoots)
}

func newAllocator(vpnsDir, routeTablesPath string, executor CommandExecutor, configRoots []string) (*Allocator, error) {
	trimmedDir := strings.TrimSpace(vpnsDir)
	if trimmedDir == "" {
		return nil, fmt.Errorf("vpns directory is required")
	}
	if routeTablesPath == "" {
		routeTablesPath = "/etc/iproute2/rt_tables"
	}
	if executor == nil {
		executor = systemCommandExecutor{}
	}

	a := &Allocator{
		vpnsDir:         trimmedDir,
		routeTablesPath: routeTablesPath,
		configRoots:     normalizeConfigRoots(trimmedDir, configRoots),
		exec:            executor,
		usedTables:      make(map[int]struct{}),
		usedMarks:       make(map[uint32]struct{}),
		stickyTables:    make(map[int]struct{}),
		stickyMarks:     make(map[uint32]struct{}),
	}
	if err := a.seedUsedValues(); err != nil {
		return nil, err
	}
	return a, nil
}

func normalizeConfigRoots(primary string, extras []string) []string {
	roots := make([]string, 0, len(extras)+1)
	seen := make(map[string]struct{}, len(extras)+1)

	add := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		if _, ok := seen[trimmed]; ok {
			return
		}
		seen[trimmed] = struct{}{}
		roots = append(roots, trimmed)
	}

	add(primary)
	for _, root := range extras {
		add(root)
	}
	return roots
}

// AllocateTable allocates a free route table ID >= 200.
func (a *Allocator) AllocateTable() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshLiveReservationsLocked()

	for candidate := minRouteTableID; candidate <= maxRouteTableID; candidate++ {
		if _, used := a.usedTables[candidate]; used {
			continue
		}
		a.usedTables[candidate] = struct{}{}
		return candidate, nil
	}
	return 0, ErrAllocationExhausted
}

// AllocateMark allocates a free fwmark >= 200.
func (a *Allocator) AllocateMark() (uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshLiveReservationsLocked()

	for candidate := uint32(minFWMark); candidate <= uint32(maxFWMark); candidate++ {
		if _, used := a.usedMarks[candidate]; used {
			continue
		}
		a.usedMarks[candidate] = struct{}{}
		return candidate, nil
	}
	return 0, ErrAllocationExhausted
}

// Reserve marks existing table/mark values as used.
func (a *Allocator) Reserve(table int, mark uint32) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refreshLiveReservationsLocked()

	if table > 0 {
		if table < minRouteTableID {
			return fmt.Errorf("%w: route table %d is below minimum %d", ErrAllocationConflict, table, minRouteTableID)
		}
		if _, used := a.usedTables[table]; used {
			return fmt.Errorf("%w: route table %d already in use", ErrAllocationConflict, table)
		}
		a.usedTables[table] = struct{}{}
	}

	if mark > 0 {
		if mark < minFWMark {
			if table > 0 {
				delete(a.usedTables, table)
			}
			return fmt.Errorf("%w: fwmark %d is below minimum %d", ErrAllocationConflict, mark, minFWMark)
		}
		if _, used := a.usedMarks[mark]; used {
			if table > 0 {
				delete(a.usedTables, table)
			}
			return fmt.Errorf("%w: fwmark 0x%x already in use", ErrAllocationConflict, mark)
		}
		a.usedMarks[mark] = struct{}{}
	}

	return nil
}

// Release releases previously reserved allocations.
func (a *Allocator) Release(table int, mark uint32) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if table > 0 {
		if _, sticky := a.stickyTables[table]; !sticky {
			delete(a.usedTables, table)
		}
	}
	if mark > 0 {
		if _, sticky := a.stickyMarks[mark]; !sticky {
			delete(a.usedMarks, mark)
		}
	}
}

func (a *Allocator) seedUsedValues() error {
	if err := a.seedFromRouteTables(); err != nil {
		return err
	}
	a.seedFromIPRules()
	a.seedFromIPRoutes()
	if err := a.seedFromPersistedConfigs(); err != nil {
		return err
	}
	return nil
}

func (a *Allocator) refreshLiveReservationsLocked() {
	// Keep allocations current even when UniFi updates route/rule state after app startup.
	_ = a.seedFromRouteTables()
	a.seedFromIPRules()
	a.seedFromIPRoutes()
}

func (a *Allocator) seedFromRouteTables() error {
	file, err := os.Open(a.routeTablesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tableID, err := strconv.Atoi(fields[0])
		if err != nil || tableID < minRouteTableID {
			continue
		}
		a.markTableUsed(tableID, true)
	}
	return scanner.Err()
}

func (a *Allocator) seedFromIPRules() {
	for _, args := range [][]string{{"rule", "show"}, {"-6", "rule", "show"}} {
		output, err := a.exec.CombinedOutput("ip", args...)
		if err != nil {
			continue
		}
		a.parseIPRulesOutput(string(output))
	}
}

func (a *Allocator) parseIPRulesOutput(output string) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for i := 0; i < len(fields)-1; i++ {
			switch fields[i] {
			case "fwmark":
				if mark, ok := parseMarkToken(fields[i+1]); ok && mark >= minFWMark {
					a.markMarkUsed(mark, true)
				}
			case "lookup", "table":
				tableID, ok := parseTableToken(fields[i+1])
				if !ok || tableID < minRouteTableID {
					continue
				}
				a.markTableUsed(tableID, true)
			}
		}
	}
}

func (a *Allocator) seedFromIPRoutes() {
	for _, args := range [][]string{{"route", "show", "table", "all"}, {"-6", "route", "show", "table", "all"}} {
		output, err := a.exec.CombinedOutput("ip", args...)
		if err != nil {
			continue
		}
		a.parseIPRoutesOutput(string(output))
	}
}

func (a *Allocator) parseIPRoutesOutput(output string) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] != "table" {
				continue
			}
			tableID, ok := parseTableToken(fields[i+1])
			if !ok || tableID < minRouteTableID {
				continue
			}
			a.markTableUsed(tableID, true)
		}
	}
}

func (a *Allocator) seedFromPersistedConfigs() error {
	for _, root := range a.configRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		sticky := root != a.vpnsDir
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name(), "vpn.conf")
			values, err := parseVPNConf(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			if table, err := strconv.Atoi(strings.TrimSpace(values["ROUTE_TABLE"])); err == nil && table >= minRouteTableID {
				a.markTableUsed(table, sticky)
			}
			if mark, ok := parseMarkToken(values["MARK"]); ok && mark >= minFWMark {
				a.markMarkUsed(mark, sticky)
			}
		}
	}
	return nil
}

func parseMarkToken(raw string) (uint32, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	if slash := strings.Index(trimmed, "/"); slash >= 0 {
		trimmed = strings.TrimSpace(trimmed[:slash])
	}
	value, err := strconv.ParseUint(trimmed, 0, 32)
	if err != nil {
		return 0, false
	}
	return uint32(value), true
}

func parseTableToken(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	end := 0
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	value, err := strconv.Atoi(trimmed[:end])
	if err != nil {
		return 0, false
	}
	return value, true
}

func (a *Allocator) markTableUsed(table int, sticky bool) {
	if table <= 0 {
		return
	}
	a.usedTables[table] = struct{}{}
	if sticky {
		a.stickyTables[table] = struct{}{}
	}
}

func (a *Allocator) markMarkUsed(mark uint32, sticky bool) {
	if mark == 0 {
		return
	}
	a.usedMarks[mark] = struct{}{}
	if sticky {
		a.stickyMarks[mark] = struct{}{}
	}
}
