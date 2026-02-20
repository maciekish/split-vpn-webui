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
	exec            CommandExecutor

	usedTables map[int]struct{}
	usedMarks  map[uint32]struct{}
}

// NewAllocator creates an allocator using live system information.
func NewAllocator(vpnsDir string) (*Allocator, error) {
	return NewAllocatorWithDeps(vpnsDir, "/etc/iproute2/rt_tables", systemCommandExecutor{})
}

// NewAllocatorWithDeps creates an allocator with custom dependencies for tests.
func NewAllocatorWithDeps(vpnsDir, routeTablesPath string, executor CommandExecutor) (*Allocator, error) {
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
		exec:            executor,
		usedTables:      make(map[int]struct{}),
		usedMarks:       make(map[uint32]struct{}),
	}
	if err := a.seedUsedValues(); err != nil {
		return nil, err
	}
	return a, nil
}

// AllocateTable allocates a free route table ID >= 200.
func (a *Allocator) AllocateTable() (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

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
		delete(a.usedTables, table)
	}
	if mark > 0 {
		delete(a.usedMarks, mark)
	}
}

func (a *Allocator) seedUsedValues() error {
	if err := a.seedFromRouteTables(); err != nil {
		return err
	}
	a.seedFromIPRules()
	if err := a.seedFromPersistedConfigs(); err != nil {
		return err
	}
	return nil
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
		a.usedTables[tableID] = struct{}{}
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
					a.usedMarks[mark] = struct{}{}
				}
			case "lookup", "table":
				tableID, err := strconv.Atoi(fields[i+1])
				if err != nil || tableID < minRouteTableID {
					continue
				}
				a.usedTables[tableID] = struct{}{}
			}
		}
	}
}

func (a *Allocator) seedFromPersistedConfigs() error {
	entries, err := os.ReadDir(a.vpnsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(a.vpnsDir, entry.Name(), "vpn.conf")
		values, err := parseVPNConf(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if table, err := strconv.Atoi(strings.TrimSpace(values["ROUTE_TABLE"])); err == nil && table >= minRouteTableID {
			a.usedTables[table] = struct{}{}
		}
		if mark, ok := parseMarkToken(values["MARK"]); ok && mark >= minFWMark {
			a.usedMarks[mark] = struct{}{}
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
