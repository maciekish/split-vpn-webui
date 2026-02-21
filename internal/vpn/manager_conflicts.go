package vpn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const systemdUnitDir = "/etc/systemd/system"

type externalVPNAllocation struct {
	name       string
	device     string
	routeTable int
	mark       uint32
}

func (m *Manager) ensureInterfaceUniqueLocked(name, iface string, existing *VPNProfile) error {
	if err := m.ensureInterfaceUniqueAmongManagedLocked(name, iface); err != nil {
		return err
	}
	if err := m.ensureInterfaceUniqueAgainstSystemLocked(iface, existing); err != nil {
		return err
	}
	if err := m.ensureInterfaceNotReservedByWGQuickLocked(iface); err != nil {
		return err
	}

	peacey, err := m.readPeaceyAllocationsLocked()
	if err != nil {
		return err
	}
	for _, allocation := range peacey {
		if strings.EqualFold(allocation.device, iface) {
			return fmt.Errorf("%w: interface %q conflicts with peacey/split-vpn profile %q", ErrVPNValidation, iface, allocation.name)
		}
	}
	return nil
}

func (m *Manager) ensureInterfaceUniqueAmongManagedLocked(name, iface string) error {
	entries, err := os.ReadDir(m.vpnsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == name {
			continue
		}
		values, err := parseVPNConf(filepath.Join(m.vpnsDir, entry.Name(), "vpn.conf"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		candidate := strings.TrimSpace(values["DEV"])
		if candidate != "" && strings.EqualFold(candidate, iface) {
			return fmt.Errorf("%w: interface %q already used by managed vpn %q", ErrVPNValidation, iface, entry.Name())
		}
	}
	return nil
}

func (m *Manager) ensureInterfaceUniqueAgainstSystemLocked(iface string, existing *VPNProfile) error {
	if m.listInterfaces == nil {
		return nil
	}
	ifaces, err := m.listInterfaces()
	if err != nil {
		return fmt.Errorf("list system interfaces: %w", err)
	}

	for _, candidate := range ifaces {
		if !strings.EqualFold(candidate.Name, iface) {
			continue
		}
		if existing != nil && strings.EqualFold(strings.TrimSpace(existing.InterfaceName), iface) {
			return nil
		}
		return fmt.Errorf("%w: interface %q already exists on system", ErrVPNValidation, iface)
	}
	return nil
}

func (m *Manager) ensureInterfaceNotReservedByWGQuickLocked(iface string) error {
	entries, err := os.ReadDir(systemdUnitDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		unit := entry.Name()
		if !strings.HasPrefix(unit, "wg-quick@") || !strings.HasSuffix(unit, ".service") {
			continue
		}
		managedIface := strings.TrimSuffix(strings.TrimPrefix(unit, "wg-quick@"), ".service")
		if strings.EqualFold(strings.TrimSpace(managedIface), iface) {
			return fmt.Errorf("%w: interface %q conflicts with existing system unit %q", ErrVPNValidation, iface, unit)
		}
	}
	return nil
}

func (m *Manager) ensureAllocationNoPeaceyConflictLocked(routeTable int, mark uint32) error {
	if routeTable <= 0 && mark == 0 {
		return nil
	}
	peacey, err := m.readPeaceyAllocationsLocked()
	if err != nil {
		return err
	}
	for _, allocation := range peacey {
		if routeTable > 0 && allocation.routeTable == routeTable {
			return fmt.Errorf("%w: route table %d conflicts with peacey/split-vpn profile %q", ErrAllocationConflict, routeTable, allocation.name)
		}
		if mark > 0 && allocation.mark == mark {
			return fmt.Errorf("%w: fwmark 0x%x conflicts with peacey/split-vpn profile %q", ErrAllocationConflict, mark, allocation.name)
		}
	}
	return nil
}

func (m *Manager) readPeaceyAllocationsLocked() ([]externalVPNAllocation, error) {
	root := strings.TrimSpace(m.peaceyDir)
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	allocations := make([]externalVPNAllocation, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		values, err := parseVPNConf(filepath.Join(root, entry.Name(), "vpn.conf"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		allocation := externalVPNAllocation{
			name:   entry.Name(),
			device: strings.TrimSpace(values["DEV"]),
		}
		if table, err := strconv.Atoi(strings.TrimSpace(values["ROUTE_TABLE"])); err == nil && table > 0 {
			allocation.routeTable = table
		}
		if mark, ok := parseMarkToken(values["MARK"]); ok && mark > 0 {
			allocation.mark = mark
		}
		if allocation.device == "" && allocation.routeTable == 0 && allocation.mark == 0 {
			continue
		}
		allocations = append(allocations, allocation)
	}
	return allocations, nil
}
