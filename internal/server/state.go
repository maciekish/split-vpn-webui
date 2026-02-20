package server

import (
	"log"
	"sort"
	"strings"
	"time"

	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/latency"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/stats"
	"split-vpn-webui/internal/util"
)

func (s *Server) refreshState() error {
	if _, err := s.configManager.Discover(); err != nil {
		// Non-fatal: directory may not exist yet on first boot.
		log.Printf("config discovery warning: %v", err)
	}
	configs, err := s.configManager.List()
	if err != nil {
		return err
	}
	vpnInterfaces := make(map[string]string)
	latencyTargets := make(map[string]latency.Target)
	resolvedGateways := make(map[string]string)
	wanCandidates := make(map[string]int)

	for _, cfg := range configs {
		if cfg.InterfaceName != "" {
			vpnInterfaces[cfg.Name] = cfg.InterfaceName
		}
		resolved := s.resolveGateway(cfg)
		resolvedGateways[cfg.Name] = resolved
		if resolved != "" {
			latencyTargets[cfg.Name] = latency.Target{
				Interface: cfg.InterfaceName,
				Address:   resolved,
			}
		}
		if wan := cfg.RawValues["WAN_INTERFACE"]; wan != "" {
			wanCandidates[wan]++
		}
	}

	s.gatewayMu.Lock()
	s.gateways = resolvedGateways
	s.gatewayMu.Unlock()

	s.latency.UpdateTargets(latencyTargets)

	storedSettings, err := s.settings.Get()
	if err != nil {
		storedSettings = settings.Settings{}
	}

	wan := storedSettings.WANInterface
	if wan == "" {
		wan = s.statsWAN()
	}
	if wan == "" {
		wan = dominantKey(wanCandidates)
	}
	if wan == "" {
		if detected, err := util.DetectWANInterface(); err == nil {
			wan = detected
		}
	}

	s.stats.ConfigureInterfaces(wan, vpnInterfaces)
	if storedSettings.WANInterface == "" {
		s.stats.SetWANInterface(wan)
	}
	return nil
}

func (s *Server) resolveGateway(cfg *config.VPNConfig) string {
	if cfg == nil {
		return ""
	}
	if gateway := strings.TrimSpace(cfg.Gateway); gateway != "" {
		return gateway
	}
	if cfg.InterfaceName == "" {
		return ""
	}
	gateway, err := util.DetectInterfaceGateway(cfg.InterfaceName)
	if err != nil {
		return ""
	}
	return gateway
}

func (s *Server) statsWAN() string {
	snap := s.stats.Snapshot()
	for _, iface := range snap.Interfaces {
		if iface.Type == stats.InterfaceWAN {
			return iface.Interface
		}
	}
	return ""
}

func (s *Server) collectConfigStatuses() ([]*config.VPNConfig, []ConfigStatus, map[string]string) {
	configs, err := s.configManager.List()
	if err != nil {
		return nil, nil, map[string]string{"configs": err.Error()}
	}
	autostart, err := s.configManager.AllAutostart()
	errMap := map[string]string{}
	if err != nil {
		errMap["autostart"] = err.Error()
	}
	s.gatewayMu.RLock()
	gatewayCopy := make(map[string]string, len(s.gateways))
	for name, value := range s.gateways {
		gatewayCopy[name] = value
	}
	s.gatewayMu.RUnlock()
	statuses := make([]ConfigStatus, 0, len(configs))
	for _, cfg := range configs {
		enabled := autostart[cfg.Name]
		connected, state, _ := util.InterfaceOperState(cfg.InterfaceName)
		gateway := gatewayCopy[cfg.Name]
		if gateway == "" {
			gateway = cfg.Gateway
		}
		statuses = append(statuses, ConfigStatus{
			Name:          cfg.Name,
			Path:          cfg.Path,
			InterfaceName: cfg.InterfaceName,
			VPNType:       cfg.VPNType,
			Gateway:       gateway,
			Autostart:     enabled,
			Connected:     connected,
			OperState:     state,
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return configs, statuses, errMap
}

func (s *Server) startVPN(cfg *config.VPNConfig) {
	if s.systemd == nil {
		s.broadcastUpdate(map[string]string{cfg.Name: "systemd manager unavailable"})
		return
	}
	if err := s.systemd.Start(vpnServiceUnitName(cfg.Name)); err != nil {
		s.broadcastUpdate(map[string]string{cfg.Name: err.Error()})
	} else {
		time.Sleep(2 * time.Second)
		s.broadcastUpdate(nil)
	}
}

func (s *Server) stopVPN(cfg *config.VPNConfig) {
	if s.systemd == nil {
		s.broadcastUpdate(map[string]string{cfg.Name: "systemd manager unavailable"})
		return
	}
	if err := s.systemd.Stop(vpnServiceUnitName(cfg.Name)); err != nil {
		s.broadcastUpdate(map[string]string{cfg.Name: err.Error()})
	} else {
		time.Sleep(1 * time.Second)
		s.broadcastUpdate(nil)
	}
}

func (s *Server) restartVPN(name string) {
	if s.systemd == nil {
		s.broadcastUpdate(map[string]string{name: "systemd manager unavailable"})
		return
	}
	if err := s.systemd.Restart(vpnServiceUnitName(name)); err != nil {
		s.broadcastUpdate(map[string]string{name: err.Error()})
		return
	}
	time.Sleep(2 * time.Second)
	s.broadcastUpdate(nil)
}

func (s *Server) applyAutostart() {
	configs, err := s.configManager.List()
	if err != nil {
		return
	}
	for _, cfg := range configs {
		enabled, err := s.configManager.AutostartEnabled(cfg.Name)
		if err != nil || !enabled {
			continue
		}
		connected, _, _ := util.InterfaceOperState(cfg.InterfaceName)
		if !connected {
			go s.startVPN(cfg)
		}
	}
}
