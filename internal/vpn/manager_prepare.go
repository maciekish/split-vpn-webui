package vpn

import (
	"fmt"
	"strconv"
	"strings"
)

func (m *Manager) prepareProfileLocked(name string, req UpsertRequest, existing *VPNProfile) (*preparedProfile, error) {
	vpnType, provider, err := m.resolveProvider(req.Type, existing)
	if err != nil {
		return nil, err
	}

	rawConfig := strings.TrimSpace(req.Config)
	if rawConfig == "" && existing != nil {
		rawConfig = existing.RawConfig
	}
	if rawConfig == "" {
		return nil, fmt.Errorf("%w: vpn config must not be empty", ErrVPNValidation)
	}

	parsed, err := provider.ParseConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	parsed.Name = name
	parsed.Type = vpnType

	configFileName, err := resolveConfigFileName(req.ConfigFile, existing, name, vpnType)
	if err != nil {
		return nil, err
	}

	iface, err := resolveInterfaceName(req.InterfaceName, existing, parsed, name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureInterfaceUniqueLocked(name, iface, existing); err != nil {
		return nil, err
	}

	routeTable, reservedTable, releaseTable, err := m.resolveRouteTableLocked(parsed, existing)
	if err != nil {
		return nil, err
	}

	mark, reservedMark, releaseMark, err := m.resolveMarkLocked(existing)
	if err != nil {
		if reservedTable > 0 {
			m.allocator.Release(reservedTable, 0)
		}
		return nil, err
	}
	if err := m.ensureAllocationNoPeaceyConflictLocked(routeTable, mark); err != nil {
		if reservedTable > 0 || reservedMark > 0 {
			m.allocator.Release(reservedTable, reservedMark)
		}
		return nil, err
	}

	sanitizedConfig := rawConfig
	warnings := []string{}
	if vpnType == "wireguard" {
		sanitizedConfig, warnings, err = sanitizeWireGuardConfig(rawConfig, routeTable)
		if err != nil {
			if reservedTable > 0 || reservedMark > 0 {
				m.allocator.Release(reservedTable, reservedMark)
			}
			return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
		}
	}

	meta := VPNMeta{
		"VPN_PROVIDER":  providerConfigValue(vpnType),
		"DEV":           iface,
		"ROUTE_TABLE":   strconv.Itoa(routeTable),
		"MARK":          fmt.Sprintf("0x%x", mark),
		"FORCED_IPSETS": "",
		"CONFIG_FILE":   configFileName,
	}
	if parsed.Gateway != "" {
		if strings.Contains(parsed.Gateway, ":") {
			meta["VPN_ENDPOINT_IPV6"] = parsed.Gateway
		} else {
			meta["VPN_ENDPOINT_IPV4"] = parsed.Gateway
		}
	}
	bound := strings.TrimSpace(req.BoundInterface)
	if bound == "" && existing != nil {
		bound = existing.BoundInterface
	}
	if bound != "" {
		meta["VPN_BOUND_IFACE"] = bound
	}

	unitProfile := &VPNProfile{
		Name:          name,
		Type:          vpnType,
		ConfigFile:    configFileName,
		InterfaceName: iface,
	}

	return &preparedProfile{
		meta:               meta,
		rawConfig:          sanitizedConfig,
		configFileName:     configFileName,
		warnings:           warnings,
		routeTableReserved: reservedTable,
		markReserved:       reservedMark,
		releaseTable:       releaseTable,
		releaseMark:        releaseMark,
		unitName:           vpnServiceUnitName(name),
		unitContent:        provider.GenerateUnit(unitProfile, m.dataDir),
	}, nil
}

func (m *Manager) resolveProvider(rawType string, existing *VPNProfile) (string, Provider, error) {
	vpnType := normalizeVPNType(rawType)
	if vpnType == "" && existing != nil {
		vpnType = normalizeVPNType(existing.Type)
	}
	provider, ok := m.providers[vpnType]
	if !ok {
		return "", nil, fmt.Errorf("%w: unsupported vpn type %q", ErrVPNValidation, rawType)
	}
	return vpnType, provider, nil
}

func (m *Manager) resolveRouteTableLocked(parsed *VPNProfile, existing *VPNProfile) (int, int, int, error) {
	if parsed != nil && parsed.RouteTable > 0 {
		if existing != nil && parsed.RouteTable == existing.RouteTable {
			return parsed.RouteTable, 0, 0, nil
		}
		if err := m.allocator.Reserve(parsed.RouteTable, 0); err != nil {
			return 0, 0, 0, err
		}
		release := 0
		if existing != nil && existing.RouteTable > 0 {
			release = existing.RouteTable
		}
		return parsed.RouteTable, parsed.RouteTable, release, nil
	}

	if existing != nil && existing.RouteTable > 0 {
		return existing.RouteTable, 0, 0, nil
	}

	table, err := m.allocator.AllocateTable()
	if err != nil {
		return 0, 0, 0, err
	}
	return table, table, 0, nil
}

func (m *Manager) resolveMarkLocked(existing *VPNProfile) (uint32, uint32, uint32, error) {
	if existing != nil && existing.FWMark >= minFWMark {
		return existing.FWMark, 0, 0, nil
	}
	mark, err := m.allocator.AllocateMark()
	if err != nil {
		return 0, 0, 0, err
	}
	return mark, mark, 0, nil
}
