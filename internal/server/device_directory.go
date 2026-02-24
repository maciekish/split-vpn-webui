package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type deviceDirectory struct {
	byMAC   map[string]string
	byIP    map[string]string
	ipsMAC  map[string]map[string]struct{}
	macByIP map[string]string
	order   []string
	seenMAC map[string]struct{}
}

func loadDeviceDirectory(ctx context.Context) deviceDirectory {
	directory := deviceDirectory{
		byMAC:   make(map[string]string),
		byIP:    make(map[string]string),
		ipsMAC:  make(map[string]map[string]struct{}),
		macByIP: make(map[string]string),
		order:   make([]string, 0),
		seenMAC: make(map[string]struct{}),
	}
	loadDHCPLeaseDeviceNames(&directory)
	loadUDAPIClientDeviceNames(ctx, &directory)
	return directory
}

func (d *deviceDirectory) addMACName(mac, name string) {
	d.ensureMaps()
	normalizedMAC := normalizeMAC(mac)
	normalizedName := normalizeDeviceName(name)
	if normalizedMAC == "" {
		return
	}
	d.ensureOrderedMAC(normalizedMAC)
	if normalizedName == "" {
		return
	}
	d.byMAC[normalizedMAC] = normalizedName
}

func (d *deviceDirectory) addIPName(ip, name string) {
	d.ensureMaps()
	normalizedIP := normalizeIP(ip)
	normalizedName := normalizeDeviceName(name)
	if normalizedIP == "" || normalizedName == "" {
		return
	}
	d.byIP[normalizedIP] = normalizedName
}

func (d *deviceDirectory) addMACIP(mac, ip string) {
	d.ensureMaps()
	normalizedMAC := normalizeMAC(mac)
	normalizedIP := normalizeIP(ip)
	if normalizedMAC == "" || normalizedIP == "" {
		return
	}
	d.ensureOrderedMAC(normalizedMAC)
	bucket := d.ipsMAC[normalizedMAC]
	if bucket == nil {
		bucket = make(map[string]struct{})
		d.ipsMAC[normalizedMAC] = bucket
	}
	bucket[normalizedIP] = struct{}{}
	d.macByIP[normalizedIP] = normalizedMAC
}

func (d *deviceDirectory) ensureOrderedMAC(mac string) {
	d.ensureMaps()
	if _, exists := d.seenMAC[mac]; exists {
		return
	}
	d.seenMAC[mac] = struct{}{}
	d.order = append(d.order, mac)
}

func (d *deviceDirectory) ensureMaps() {
	if d.byMAC == nil {
		d.byMAC = make(map[string]string)
	}
	if d.byIP == nil {
		d.byIP = make(map[string]string)
	}
	if d.ipsMAC == nil {
		d.ipsMAC = make(map[string]map[string]struct{})
	}
	if d.macByIP == nil {
		d.macByIP = make(map[string]string)
	}
	if d.order == nil {
		d.order = make([]string, 0)
	}
	if d.seenMAC == nil {
		d.seenMAC = make(map[string]struct{})
	}
}

func (d *deviceDirectory) lookupMAC(mac string) (string, []string) {
	normalizedMAC := normalizeMAC(mac)
	if normalizedMAC == "" {
		return "", nil
	}
	name := d.byMAC[normalizedMAC]
	values := d.ipsMAC[normalizedMAC]
	if len(values) == 0 {
		return name, nil
	}
	ips := make([]string, 0, len(values))
	for ip := range values {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return name, ips
}

func (d *deviceDirectory) lookupIP(value string) string {
	return d.byIP[normalizeIP(value)]
}

func (d *deviceDirectory) lookupIPMAC(value string) string {
	return d.macByIP[normalizeIP(value)]
}

type discoveredDevice struct {
	MAC        string   `json:"mac"`
	Name       string   `json:"name,omitempty"`
	IPHints    []string `json:"ipHints,omitempty"`
	SearchText string   `json:"searchText,omitempty"`
}

func (d *deviceDirectory) listDevices() []discoveredDevice {
	devices := make([]discoveredDevice, 0, len(d.order))
	for _, mac := range d.order {
		name := strings.TrimSpace(d.byMAC[mac])
		ipsMap := d.ipsMAC[mac]
		ips := make([]string, 0, len(ipsMap))
		for ip := range ipsMap {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		searchParts := []string{mac}
		if name != "" {
			searchParts = append(searchParts, name)
		}
		searchParts = append(searchParts, ips...)
		devices = append(devices, discoveredDevice{
			MAC:        mac,
			Name:       name,
			IPHints:    ips,
			SearchText: strings.ToLower(strings.Join(searchParts, " ")),
		})
	}
	return devices
}

func normalizeMAC(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := net.ParseMAC(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.String())
}

func normalizeIP(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "%"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.String()
	}
	ip, _, err := net.ParseCIDR(trimmed)
	if err != nil {
		return ""
	}
	return ip.String()
}

func normalizeDeviceName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.Trim(trimmed, "\"'")
	if trimmed == "" || trimmed == "*" {
		return ""
	}
	return trimmed
}

func loadDHCPLeaseDeviceNames(directory *deviceDirectory) {
	paths := []string{
		"/tmp/dhcp.leases",
		"/run/dnsmasq.leases",
		"/var/lib/misc/dnsmasq.leases",
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil || len(content) == 0 {
			continue
		}
		for _, row := range parseDHCPLeaseRows(string(content)) {
			directory.addMACName(row.MAC, row.Hostname)
			directory.addIPName(row.IP, row.Hostname)
			directory.addMACIP(row.MAC, row.IP)
		}
	}
}

type dhcpLeaseRow struct {
	MAC      string
	IP       string
	Hostname string
}

func parseDHCPLeaseRows(raw string) []dhcpLeaseRow {
	lines := strings.Split(raw, "\n")
	rows := make([]dhcpLeaseRow, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 4 {
			continue
		}
		mac := normalizeMAC(fields[1])
		ip := normalizeIP(fields[2])
		host := normalizeDeviceName(fields[3])
		if mac == "" || ip == "" {
			continue
		}
		rows = append(rows, dhcpLeaseRow{
			MAC:      mac,
			IP:       ip,
			Hostname: host,
		})
	}
	return rows
}

func loadUDAPIClientDeviceNames(ctx context.Context, directory *deviceDirectory) {
	commands := [][]string{
		{"ubios-udapi-client", "GET", "/services"},
		{"ubios-udapi-client", "get", "-r", "/services"},
		{"ubios-udapi-client", "GET", "/clients"},
		{"ubios-udapi-client", "get", "-r", "/clients"},
	}
	for _, command := range commands {
		payload, err := commandJSON(ctx, command)
		if err != nil {
			continue
		}
		ingestDevicePayload(payload, directory)
	}
}

func commandJSON(ctx context.Context, argv []string) (any, error) {
	if len(argv) == 0 {
		return nil, exec.ErrNotFound
	}
	runCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(runCtx, argv[0], argv[1:]...).Output()
	if err != nil {
		return nil, err
	}
	payload := bytes.TrimSpace(output)
	if len(payload) == 0 {
		return nil, fmt.Errorf("command returned empty output")
	}
	start := bytes.IndexAny(payload, "[{")
	if start < 0 {
		return nil, fmt.Errorf("command output did not contain JSON")
	}
	payload = payload[start:]
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func ingestDevicePayload(value any, directory *deviceDirectory) {
	switch typed := value.(type) {
	case []any:
		for _, entry := range typed {
			ingestDevicePayload(entry, directory)
		}
	case map[string]any:
		lowered := lowercaseJSONMap(typed)
		mac := pickDeviceMAC(lowered)
		name := pickDeviceName(lowered)
		ip := pickDeviceIP(lowered)
		if mac != "" {
			directory.addMACName(mac, name)
			directory.addMACIP(mac, ip)
		}
		if ip != "" && name != "" {
			directory.addIPName(ip, name)
		}
		for _, entry := range typed {
			ingestDevicePayload(entry, directory)
		}
	}
}

func lowercaseJSONMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return out
}

func pickDeviceMAC(values map[string]any) string {
	for _, key := range []string{"mac", "macaddress", "mac_address", "clientmac", "hwaddr"} {
		if mac := normalizeMAC(stringValue(values[key])); mac != "" {
			return mac
		}
	}
	return ""
}

func pickDeviceName(values map[string]any) string {
	for _, key := range []string{
		"displayname",
		"display_name",
		"alias",
		"name",
		"clientname",
		"client_name",
		"hostname",
		"host_name",
		"fixeddnsname",
		"fixed_dns_name",
	} {
		if name := normalizeDeviceName(stringValue(values[key])); name != "" {
			return name
		}
	}
	return ""
}

func pickDeviceIP(values map[string]any) string {
	for _, key := range []string{
		"ip",
		"ipaddress",
		"ip_address",
		"address",
		"hostip",
		"host_ip",
		"fixedip",
		"fixed_ip",
		"ipv4",
		"ipv6",
	} {
		if ip := normalizeIP(stringValue(values[key])); ip != "" {
			return ip
		}
	}
	return ""
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, entry := range typed {
			if asString := stringValue(entry); asString != "" {
				return asString
			}
		}
		return ""
	default:
		return ""
	}
}
