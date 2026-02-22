package vpn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type mockCommandExecutor struct {
	outputs map[string][]byte
	errs    map[string]error
}

func (m mockCommandExecutor) CombinedOutput(name string, args ...string) ([]byte, error) {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if err, ok := m.errs[key]; ok {
		return nil, err
	}
	if out, ok := m.outputs[key]; ok {
		return out, nil
	}
	return nil, errors.New("command not mocked")
}

func TestAllocatorStartsAt200(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip"),
			"ip -6 rule show": errors.New("missing ip"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	table, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable failed: %v", err)
	}
	if table != 200 {
		t.Fatalf("expected first table to be 200, got %d", table)
	}

	mark, err := alloc.AllocateMark()
	if err != nil {
		t.Fatalf("AllocateMark failed: %v", err)
	}
	if mark != 200 {
		t.Fatalf("expected first mark to be 200, got %d", mark)
	}
}

func TestAllocatorAvoidsCollisionsFromSystemState(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	routeTablesContent := "200 custom200\n201 custom201\n"
	if err := os.WriteFile(routeTables, []byte(routeTablesContent), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{
			"ip rule show":    []byte("32765: from all fwmark 0xc8 lookup 205.eth8\n"),
			"ip -6 rule show": []byte("32764: from all fwmark 0x12d lookup 210.eth8\n"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	table, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable failed: %v", err)
	}
	if table != 202 {
		t.Fatalf("expected first free table to be 202, got %d", table)
	}

	mark, err := alloc.AllocateMark()
	if err != nil {
		t.Fatalf("AllocateMark failed: %v", err)
	}
	if mark != 201 {
		t.Fatalf("expected first free mark to be 201, got %d", mark)
	}
}

func TestAllocatorAvoidsCollisionsFromExistingRouteEntries(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{
			"ip route show table all":    []byte("default dev eth8 table 200.eth8\n10.0.0.0/24 dev br0 table 201.eth8\n"),
			"ip -6 route show table all": []byte("default via fe80::1 dev eth8 table 202.eth8\n"),
		},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip rules"),
			"ip -6 rule show": errors.New("missing ip6 rules"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	table, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable failed: %v", err)
	}
	if table != 203 {
		t.Fatalf("expected first free table to be 203, got %d", table)
	}
}

func TestAllocatorReleaseDoesNotFreeStickyExternalReservations(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{
			"ip rule show": []byte("32765: from all fwmark 0xc9 lookup 201.eth8\n"),
		},
		errs: map[string]error{
			"ip -6 rule show":            errors.New("missing ip6 rules"),
			"ip route show table all":    errors.New("missing ip routes"),
			"ip -6 route show table all": errors.New("missing ip6 routes"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	alloc.Release(201, 201)

	table, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable failed: %v", err)
	}
	if table != 200 {
		t.Fatalf("expected first free table to be 200, got %d", table)
	}

	nextTable, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable (2nd) failed: %v", err)
	}
	if nextTable != 202 {
		t.Fatalf("expected second free table to skip sticky 201 and be 202, got %d", nextTable)
	}

	mark, err := alloc.AllocateMark()
	if err != nil {
		t.Fatalf("AllocateMark failed: %v", err)
	}
	if mark != 200 {
		t.Fatalf("expected first free mark to be 200, got %d", mark)
	}

	nextMark, err := alloc.AllocateMark()
	if err != nil {
		t.Fatalf("AllocateMark (2nd) failed: %v", err)
	}
	if nextMark != 202 {
		t.Fatalf("expected second free mark to skip sticky 201 and be 202, got %d", nextMark)
	}
}

func TestAllocatorRefreshesLiveReservationsOnAllocation(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	exec := &mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip rules at startup"),
			"ip -6 rule show": errors.New("missing ip6 rules at startup"),
		},
	}
	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, exec)
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	exec.outputs["ip rule show"] = []byte("32765: from all fwmark 0xc9 lookup 201.eth8\n")
	delete(exec.errs, "ip rule show")

	first, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable failed: %v", err)
	}
	if first != 200 {
		t.Fatalf("expected first table to be 200, got %d", first)
	}

	second, err := alloc.AllocateTable()
	if err != nil {
		t.Fatalf("AllocateTable (2nd) failed: %v", err)
	}
	if second != 202 {
		t.Fatalf("expected second table to skip runtime-discovered 201 and be 202, got %d", second)
	}
}

func TestAllocatorRecoversPersistedAllocations(t *testing.T) {
	vpnsDir := t.TempDir()
	vpnDir := filepath.Join(vpnsDir, "wg-sgp")
	if err := os.MkdirAll(vpnDir, 0o700); err != nil {
		t.Fatalf("create vpn dir: %v", err)
	}
	conf := "ROUTE_TABLE=220\nMARK=0x220\n"
	if err := os.WriteFile(filepath.Join(vpnDir, "vpn.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("write vpn.conf: %v", err)
	}
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip"),
			"ip -6 rule show": errors.New("missing ip"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	if err := alloc.Reserve(220, 0x220); !errors.Is(err, ErrAllocationConflict) {
		t.Fatalf("expected allocation conflict for persisted values, got %v", err)
	}
}

func TestAllocatorAllocationsAreUnique(t *testing.T) {
	vpnsDir := t.TempDir()
	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDeps(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip"),
			"ip -6 rule show": errors.New("missing ip"),
		},
	})
	if err != nil {
		t.Fatalf("NewAllocatorWithDeps failed: %v", err)
	}

	tables := map[int]struct{}{}
	marks := map[uint32]struct{}{}
	for i := 0; i < 10; i++ {
		table, err := alloc.AllocateTable()
		if err != nil {
			t.Fatalf("AllocateTable failed: %v", err)
		}
		if _, exists := tables[table]; exists {
			t.Fatalf("duplicate table allocated: %d", table)
		}
		tables[table] = struct{}{}

		mark, err := alloc.AllocateMark()
		if err != nil {
			t.Fatalf("AllocateMark failed: %v", err)
		}
		if _, exists := marks[mark]; exists {
			t.Fatalf("duplicate mark allocated: %d", mark)
		}
		marks[mark] = struct{}{}
	}
}

func TestAllocatorScansAdditionalConfigRoots(t *testing.T) {
	vpnsDir := t.TempDir()
	peaceyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(peaceyDir, "peacey-one"), 0o700); err != nil {
		t.Fatalf("create peacey profile: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(peaceyDir, "peacey-one", "vpn.conf"),
		[]byte("ROUTE_TABLE=333\nMARK=0x333\n"),
		0o644,
	); err != nil {
		t.Fatalf("write peacey vpn.conf: %v", err)
	}

	routeTables := filepath.Join(t.TempDir(), "rt_tables")
	if err := os.WriteFile(routeTables, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write route tables file: %v", err)
	}

	alloc, err := NewAllocatorWithDepsAndConfigRoots(vpnsDir, routeTables, mockCommandExecutor{
		outputs: map[string][]byte{},
		errs: map[string]error{
			"ip rule show":    errors.New("missing ip"),
			"ip -6 rule show": errors.New("missing ip"),
		},
	}, []string{peaceyDir})
	if err != nil {
		t.Fatalf("NewAllocatorWithDepsAndConfigRoots failed: %v", err)
	}

	if err := alloc.Reserve(333, 0x333); !errors.Is(err, ErrAllocationConflict) {
		t.Fatalf("expected persisted allocation conflict from additional config root, got %v", err)
	}
}
