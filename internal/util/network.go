package util

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// InterfaceInfo summarises a network interface and its addresses.
type InterfaceInfo struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

// DetectLANInterface returns the best LAN interface candidate.
// Preference order favors UniFi bridge interfaces (br0, then br*), while
// explicitly excluding tunnel-style interface names.
func DetectLANInterface() (string, error) {
	interfaces, err := InterfacesWithAddrs()
	if err != nil {
		return "", err
	}
	name, _ := selectLANInterfaceAndIPv4(interfaces)
	if name == "" {
		return "", errors.New("lan interface not found")
	}
	return name, nil
}

// DetectLANIPv4 returns the private IPv4 address of the best LAN candidate.
func DetectLANIPv4() (string, error) {
	interfaces, err := InterfacesWithAddrs()
	if err != nil {
		return "", err
	}
	_, ip := selectLANInterfaceAndIPv4(interfaces)
	if ip == "" {
		return "", errors.New("lan ipv4 address not found")
	}
	return ip, nil
}

// DetectWANInterface attempts to determine the default WAN interface by reading /proc/net/route.
func DetectWANInterface() (string, error) {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// skip header
	if !scanner.Scan() {
		return "", errors.New("unexpected /proc/net/route format")
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 11 {
			continue
		}
		iface := fields[0]
		destination := fields[1]
		flags := fields[3]
		if destination == "00000000" && strings.Contains(flags, "2") {
			return iface, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("default route not found")
}

// InterfacesWithAddrs returns all interfaces along with their addresses.
func InterfacesWithAddrs() ([]InterfaceInfo, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	infos := make([]InterfaceInfo, 0, len(list))
	for _, iface := range list {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		addresses := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addresses = append(addresses, addr.String())
		}
		infos = append(infos, InterfaceInfo{Name: iface.Name, Addresses: addresses})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// InterfaceIPv4 returns the first IPv4 address bound to an interface.
func InterfaceIPv4(name string) (string, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", errors.New("no IPv4 address found")
}

// InterfaceOperState reports whether an interface is up and its operstate text.
func InterfaceOperState(name string) (bool, string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, "", errors.New("interface not specified")
	}
	base := filepath.Join("/sys/class/net", trimmed)
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "missing", nil
		}
		return false, "error", err
	}
	flagUp := false
	iface, err := net.InterfaceByName(trimmed)
	if err == nil {
		flagUp = iface.Flags&net.FlagUp != 0
	}
	data, err := os.ReadFile(filepath.Join(base, "operstate"))
	if err != nil {
		// Some firmware/kernel combinations expose interface state inconsistently;
		// use interface flags as a practical fallback for tunnel interfaces.
		return flagUp, "unknown", nil
	}
	state := strings.TrimSpace(string(data))
	return interfaceStateConnected(state, flagUp), state, nil
}

func interfaceStateConnected(state string, flagUp bool) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "up":
		return true
	case "unknown", "dormant":
		return flagUp
	default:
		return false
	}
}

// DetectInterfaceGateway attempts to determine the gateway for an interface.
// It first inspects `ip route` output, then falls back to guessing the `.1`
// address within the interface's IPv4 network if no explicit route is found.
func DetectInterfaceGateway(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", errors.New("interface not specified")
	}
	if gateway := gatewayFromRoute(trimmed); gateway != "" {
		return gateway, nil
	}
	ip, err := InterfaceIPv4(trimmed)
	if err != nil {
		return "", err
	}
	return guessGatewayFromIP(trimmed, ip)
}

func gatewayFromRoute(iface string) string {
	cmd := exec.Command("ip", "-4", "route", "show", "dev", iface)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "via" {
				candidate := strings.TrimSpace(fields[i+1])
				if ip := net.ParseIP(candidate); ip != nil {
					return ip.String()
				}
			}
		}
	}
	return ""
}

func guessGatewayFromIP(interfaceName, ipStr string) (string, error) {
	parsed := net.ParseIP(strings.TrimSpace(ipStr))
	if parsed == nil {
		return "", fmt.Errorf("invalid ip: %s", ipStr)
	}
	v4 := parsed.To4()
	if v4 == nil {
		return "", fmt.Errorf("not an ipv4 address: %s", ipStr)
	}
	guess := make(net.IP, len(v4))
	copy(guess, v4)
	if strings.HasPrefix(interfaceName, "wg") && len(guess) == 4 {
		guess[2] = 0
		guess[3] = 1
	} else {
		guess[3] = 1
	}
	return guess.String(), nil
}

type lanCandidate struct {
	name  string
	ip    string
	score int
}

func selectLANInterfaceAndIPv4(interfaces []InterfaceInfo) (string, string) {
	best := lanCandidate{score: 1 << 30}
	found := false

	for _, iface := range interfaces {
		name := strings.TrimSpace(iface.Name)
		if name == "" {
			continue
		}
		score, ok := lanInterfaceScore(name)
		if !ok {
			continue
		}
		ip := firstPrivateIPv4(iface.Addresses)
		if ip == "" {
			continue
		}
		candidate := lanCandidate{name: name, ip: ip, score: score}
		if !found || candidate.score < best.score || (candidate.score == best.score && candidate.name < best.name) {
			best = candidate
			found = true
		}
	}
	if !found {
		return "", ""
	}
	return best.name, best.ip
}

func lanInterfaceScore(name string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return 0, false
	}
	if strings.HasPrefix(lower, "wg") ||
		strings.HasPrefix(lower, "tun") ||
		strings.HasPrefix(lower, "tap") ||
		strings.HasPrefix(lower, "ppp") ||
		strings.HasPrefix(lower, "vpn") {
		return 0, false
	}
	switch {
	case lower == "br0":
		return 0, true
	case strings.HasPrefix(lower, "br"):
		return 1, true
	case strings.HasPrefix(lower, "lan"):
		return 2, true
	case strings.HasPrefix(lower, "eth"), strings.HasPrefix(lower, "en"):
		return 3, true
	default:
		return 4, true
	}
}

func firstPrivateIPv4(addresses []string) string {
	for _, value := range addresses {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		var ip net.IP
		if strings.Contains(trimmed, "/") {
			parsed, _, err := net.ParseCIDR(trimmed)
			if err != nil {
				continue
			}
			ip = parsed
		} else {
			ip = net.ParseIP(trimmed)
		}
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		if !v4.IsPrivate() {
			continue
		}
		return v4.String()
	}
	return ""
}
