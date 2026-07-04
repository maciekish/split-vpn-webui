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

	mssV4, err := ValidateMSSClamp(req.MSSClampV4)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	mssV6, err := ValidateMSSClamp(req.MSSClampV6)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}

	parsed, err := provider.ParseConfig(rawConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
	}
	parsed.Name = name
	parsed.Type = vpnType

	iface, err := resolveInterfaceName(req.InterfaceName, existing, parsed, name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureInterfaceUniqueLocked(name, iface, existing); err != nil {
		return nil, err
	}

	configFileName, err := resolveConfigFileName(req.ConfigFile, existing, name, vpnType, iface)
	if err != nil {
		return nil, err
	}
	parsed.ConfigFile = configFileName

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
	requiredSupportingFiles := []string{}
	if isWireGuardLike(vpnType) {
		if vpnType == "wireguard" && parsed.WireGuard != nil && HasAmneziaWGKeys(&parsed.WireGuard.Interface) {
			if reservedTable > 0 || reservedMark > 0 {
				m.allocator.Release(reservedTable, reservedMark)
			}
			return nil, fmt.Errorf("%w: config contains AmneziaWG obfuscation keys (Jc/S1/H1/...); use the AmneziaWG vpn type instead", ErrVPNValidation)
		}
		sanitizedConfig, warnings, err = sanitizeWireGuardConfig(rawConfig, routeTable, hasResolvconfBinary())
		if err != nil {
			if reservedTable > 0 || reservedMark > 0 {
				m.allocator.Release(reservedTable, reservedMark)
			}
			return nil, fmt.Errorf("%w: %v", ErrVPNValidation, err)
		}
		if vpnType == "amneziawg" {
			if parsed.AmneziaWG.IsEmpty() {
				warnings = append(warnings, "No AmneziaWG obfuscation parameters set; the tunnel will behave like vanilla WireGuard")
			}
			if parsed.AmneziaWG.UsesExtendedPadding() || parsed.AmneziaWG.UsesHeaderRanges() {
				warnings = append(warnings, "S3/S4 padding or H1-H4 ranges require the amneziawg kernel module; the tunnel will fail to start until one is installed")
			}
		}
	} else if vpnType == "openvpn" {
		requiredSupportingFiles, err = requiredOpenVPNFiles(parsed.OpenVPN)
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
	// MSS clamp is authoritative from the request (the editor always submits the
	// current value); an empty value disables clamping for that family.
	if mssV4 != "" {
		meta["MSS_CLAMPING_IPV4"] = mssV4
	}
	if mssV6 != "" {
		meta["MSS_CLAMPING_IPV6"] = mssV6
	}

	unitProfile := &VPNProfile{
		Name:          name,
		Type:          vpnType,
		ConfigFile:    configFileName,
		InterfaceName: iface,
	}

	return &preparedProfile{
		meta:                    meta,
		rawConfig:               sanitizedConfig,
		configFileName:          configFileName,
		warnings:                warnings,
		requiredSupportingFiles: requiredSupportingFiles,
		routeTableReserved:      reservedTable,
		markReserved:            reservedMark,
		releaseTable:            releaseTable,
		releaseMark:             releaseMark,
		unitName:                vpnServiceUnitName(name),
		unitContent:             provider.GenerateUnit(unitProfile, m.dataDir),
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
