package stats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLoadAverage(t *testing.T) {
	load, err := parseLoadAverage("0.12 0.34 0.56 1/234 5678\n")
	if err != nil {
		t.Fatalf("parse load average: %v", err)
	}
	if load.Load1 != 0.12 || load.Load5 != 0.34 || load.Load15 != 0.56 {
		t.Fatalf("unexpected load average: %#v", load)
	}
}

func TestParseCgroupV2CPUUsageNS(t *testing.T) {
	usageNS, err := parseCgroupV2CPUUsageNS("usage_usec 12345\nuser_usec 10000\nsystem_usec 2345\n")
	if err != nil {
		t.Fatalf("parse cgroup v2 usage: %v", err)
	}
	if usageNS != 12345000 {
		t.Fatalf("expected 12345000 ns, got %d", usageNS)
	}
}

func TestCollectorProcessCPUUsageFromCgroupV2(t *testing.T) {
	root := t.TempDir()
	unitDir := filepath.Join(root, "system.slice", "svpn-vpn-a.service")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatalf("mkdir cgroup dir: %v", err)
	}
	cpuStatPath := filepath.Join(unitDir, "cpu.stat")
	if err := os.WriteFile(cpuStatPath, []byte("usage_usec 1000000\n"), 0o644); err != nil {
		t.Fatalf("write cpu.stat: %v", err)
	}

	collector := NewCollector("", time.Second, 10)
	collector.cgroupRoot = root
	collector.ConfigureInterfaces("", map[string]string{"vpn-a": "tun0"}, map[string]string{"vpn-a": "openvpn"})
	iface := collector.interfaces["vpn-a"]
	collector.updateCPUUsageLocked(time.Unix(100, 0), iface)
	if iface.CPUUsage == nil || iface.CPUUsage.Available {
		t.Fatalf("expected first sample to establish baseline, got %#v", iface.CPUUsage)
	}

	if err := os.WriteFile(cpuStatPath, []byte("usage_usec 1250000\n"), 0o644); err != nil {
		t.Fatalf("rewrite cpu.stat: %v", err)
	}
	collector.updateCPUUsageLocked(time.Unix(101, 0), iface)
	if iface.CPUUsage == nil || !iface.CPUUsage.Available {
		t.Fatalf("expected available cpu usage, got %#v", iface.CPUUsage)
	}
	if iface.CPUUsage.Percent != 25 {
		t.Fatalf("expected 25%% CPU, got %.2f", iface.CPUUsage.Percent)
	}
}

func TestCollectorMarksWireGuardCPUKernelBacked(t *testing.T) {
	collector := NewCollector("", time.Second, 10)
	collector.ConfigureInterfaces("", map[string]string{"vpn-a": "wg0"}, map[string]string{"vpn-a": "wireguard"})
	iface := collector.interfaces["vpn-a"]
	collector.updateCPUUsageLocked(time.Unix(100, 0), iface)
	if iface.CPUUsage == nil || iface.CPUUsage.Source != "kernel" || iface.CPUUsage.Available {
		t.Fatalf("expected kernel CPU accounting marker, got %#v", iface.CPUUsage)
	}
}
