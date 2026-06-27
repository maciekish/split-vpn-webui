package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLoadAvgPath     = "/proc/loadavg"
	defaultCgroupRoot      = "/sys/fs/cgroup"
	defaultSysClassNetRoot = "/sys/class/net"
)

type cpuSource string

const (
	cpuSourceKernel  cpuSource = "kernel"
	cpuSourceProcess cpuSource = "process"
	cpuSourceUnknown cpuSource = "unknown"
)

// LoadAverage contains the system load averages from /proc/loadavg.
type LoadAverage struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// CPUUsage reports service-level CPU accounting for a VPN where available.
type CPUUsage struct {
	Percent   float64 `json:"percent"`
	Available bool    `json:"available"`
	Source    string  `json:"source"`
	Message   string  `json:"message,omitempty"`
}

func (c *Collector) updateLoadAverageLocked() {
	load, err := readLoadAverage(c.loadAvgPath)
	if err != nil {
		c.loadAverage = nil
		return
	}
	c.loadAverage = &load
}

func (c *Collector) updateCPUUsageLocked(now time.Time, iface *InterfaceStats) {
	if iface.Type != InterfaceVPN {
		iface.CPUUsage = nil
		iface.lastCPUTimeNS = 0
		iface.lastCPUSampleAt = time.Time{}
		return
	}

	switch c.cpuSourceFor(iface) {
	case cpuSourceKernel:
		iface.lastCPUTimeNS = 0
		iface.lastCPUSampleAt = time.Time{}
		iface.CPUUsage = &CPUUsage{
			Available: false,
			Source:    string(cpuSourceKernel),
			Message:   "Kernel module CPU is included in system load, not per-VPN process accounting.",
		}
	case cpuSourceProcess:
		c.updateProcessCPUUsageLocked(now, iface)
	default:
		iface.lastCPUTimeNS = 0
		iface.lastCPUSampleAt = time.Time{}
		iface.CPUUsage = &CPUUsage{
			Available: false,
			Source:    string(cpuSourceUnknown),
			Message:   "VPN CPU accounting is unavailable until the tunnel interface is active.",
		}
	}
}

func (c *Collector) updateProcessCPUUsageLocked(now time.Time, iface *InterfaceStats) {
	usageNS, err := readServiceCPUUsageNS(c.cgroupRoot, iface.Name)
	if err != nil {
		iface.lastCPUTimeNS = 0
		iface.lastCPUSampleAt = time.Time{}
		iface.CPUUsage = &CPUUsage{
			Available: false,
			Source:    string(cpuSourceProcess),
			Message:   "Service CPU accounting is unavailable.",
		}
		return
	}
	if iface.lastCPUSampleAt.IsZero() || usageNS < iface.lastCPUTimeNS {
		iface.lastCPUTimeNS = usageNS
		iface.lastCPUSampleAt = now
		iface.CPUUsage = &CPUUsage{
			Available: false,
			Source:    string(cpuSourceProcess),
			Message:   "Collecting service CPU baseline.",
		}
		return
	}
	elapsed := now.Sub(iface.lastCPUSampleAt)
	deltaNS := usageNS - iface.lastCPUTimeNS
	if elapsed <= 0 {
		iface.CPUUsage = &CPUUsage{
			Available: false,
			Source:    string(cpuSourceProcess),
			Message:   "Waiting for the next service CPU sample.",
		}
		return
	}
	iface.lastCPUTimeNS = usageNS
	iface.lastCPUSampleAt = now
	iface.CPUUsage = &CPUUsage{
		Percent:   (float64(deltaNS) / float64(elapsed.Nanoseconds())) * 100,
		Available: true,
		Source:    string(cpuSourceProcess),
	}
}

func (c *Collector) cpuSourceFor(iface *InterfaceStats) cpuSource {
	switch strings.ToLower(strings.TrimSpace(iface.VPNType)) {
	case "openvpn":
		return cpuSourceProcess
	case "wireguard":
		return cpuSourceKernel
	case "amneziawg":
		if iface.Interface == "" {
			return cpuSourceUnknown
		}
		if isTunDevice(c.sysClassNetRoot, iface.Interface) {
			return cpuSourceProcess
		}
		if interfaceExists(c.sysClassNetRoot, iface.Interface) {
			return cpuSourceKernel
		}
		return cpuSourceUnknown
	default:
		if iface.Interface != "" && isTunDevice(c.sysClassNetRoot, iface.Interface) {
			return cpuSourceProcess
		}
		return cpuSourceUnknown
	}
}

func readLoadAverage(path string) (LoadAverage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return LoadAverage{}, err
	}
	return parseLoadAverage(string(raw))
}

func parseLoadAverage(raw string) (LoadAverage, error) {
	fields := strings.Fields(raw)
	if len(fields) < 3 {
		return LoadAverage{}, fmt.Errorf("loadavg has %d fields", len(fields))
	}
	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return LoadAverage{}, err
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return LoadAverage{}, err
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return LoadAverage{}, err
	}
	return LoadAverage{Load1: load1, Load5: load5, Load15: load15}, nil
}

func readServiceCPUUsageNS(cgroupRoot, vpnName string) (uint64, error) {
	if strings.TrimSpace(vpnName) == "" || strings.ContainsAny(vpnName, `/\`) {
		return 0, fmt.Errorf("invalid vpn name %q", vpnName)
	}
	unit := "svpn-" + vpnName + ".service"
	v2Path := filepath.Join(cgroupRoot, "system.slice", unit, "cpu.stat")
	if raw, err := os.ReadFile(v2Path); err == nil {
		return parseCgroupV2CPUUsageNS(string(raw))
	}
	for _, controller := range []string{"cpuacct", "cpu,cpuacct", "cpuacct,cpu"} {
		path := filepath.Join(cgroupRoot, controller, "system.slice", unit, "cpuacct.usage")
		if raw, err := os.ReadFile(path); err == nil {
			return parseCPUAcctUsageNS(string(raw))
		}
	}
	return 0, fmt.Errorf("cpu usage not found for %s", unit)
}

func parseCgroupV2CPUUsageNS(raw string) (uint64, error) {
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "usage_usec" {
			continue
		}
		usageUS, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return usageUS * 1000, nil
	}
	return 0, fmt.Errorf("usage_usec not found")
}

func parseCPUAcctUsageNS(raw string) (uint64, error) {
	return strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
}

func isTunDevice(sysClassNetRoot, iface string) bool {
	if strings.TrimSpace(iface) == "" || strings.ContainsAny(iface, `/\`) {
		return false
	}
	_, err := os.Stat(filepath.Join(sysClassNetRoot, iface, "tun_flags"))
	return err == nil
}

func interfaceExists(sysClassNetRoot, iface string) bool {
	if strings.TrimSpace(iface) == "" || strings.ContainsAny(iface, `/\`) {
		return false
	}
	_, err := os.Stat(filepath.Join(sysClassNetRoot, iface))
	return err == nil
}

func cloneLoadAverage(load *LoadAverage) *LoadAverage {
	if load == nil {
		return nil
	}
	clone := *load
	return &clone
}
