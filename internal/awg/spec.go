// Package awg runs AmneziaWG tunnels through the embedded userspace engine
// (github.com/amnezia-vpn/amneziawg-go) or, when available, the amneziawg
// kernel module. It is consumed by the `split-vpn-webui tunnel run`
// subcommand, which systemd starts as a separate process per tunnel.
package awg

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"split-vpn-webui/internal/vpn"
)

// PeerSpec describes a single peer of the tunnel.
type PeerSpec struct {
	PublicKey           string
	PresharedKey        string
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
}

// TunnelSpec is everything needed to bring an AmneziaWG tunnel up.
type TunnelSpec struct {
	Name          string
	InterfaceName string
	MTU           int
	ListenPort    int
	PrivateKey    string
	Addresses     []string
	RouteTable    int
	Params        *vpn.AmneziaWGParams
	Peers         []PeerSpec
	PreUp         []string
	PostUp        []string
	PreDown       []string
	PostDown      []string
}

// BuildSpec converts a parsed AmneziaWG profile into a TunnelSpec.
func BuildSpec(profile *vpn.VPNProfile) (*TunnelSpec, error) {
	if profile == nil {
		return nil, fmt.Errorf("profile is required")
	}
	if profile.Type != "amneziawg" {
		return nil, fmt.Errorf("profile %q has type %q, expected amneziawg", profile.Name, profile.Type)
	}
	if profile.WireGuard == nil {
		return nil, fmt.Errorf("profile %q has no parsed config", profile.Name)
	}
	iface := strings.TrimSpace(profile.InterfaceName)
	if iface == "" {
		return nil, fmt.Errorf("profile %q has no interface name", profile.Name)
	}
	if profile.RouteTable <= 0 {
		return nil, fmt.Errorf("profile %q has no route table allocated", profile.Name)
	}

	cfg := profile.WireGuard
	spec := &TunnelSpec{
		Name:          profile.Name,
		InterfaceName: iface,
		MTU:           1420,
		PrivateKey:    cfg.Interface.PrivateKey,
		RouteTable:    profile.RouteTable,
		Params:        profile.AmneziaWG,
		PreUp:         append([]string(nil), cfg.Interface.PreUp...),
		PostUp:        append([]string(nil), cfg.Interface.PostUp...),
		PreDown:       append([]string(nil), cfg.Interface.PreDown...),
		PostDown:      append([]string(nil), cfg.Interface.PostDown...),
	}

	for _, addr := range cfg.Interface.Addresses {
		normalized, err := normalizeAddress(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid interface address %q: %v", addr, err)
		}
		spec.Addresses = append(spec.Addresses, normalized)
	}
	if len(spec.Addresses) == 0 {
		return nil, fmt.Errorf("profile %q has no interface addresses", profile.Name)
	}

	if raw := firstExtra(cfg.Interface.Extras, "mtu"); raw != "" {
		mtu, err := strconv.Atoi(raw)
		if err != nil || mtu < 576 || mtu > 65535 {
			return nil, fmt.Errorf("invalid MTU %q", raw)
		}
		spec.MTU = mtu
	}
	if raw := firstExtra(cfg.Interface.Extras, "listenport"); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil || port < 0 || port > 65535 {
			return nil, fmt.Errorf("invalid ListenPort %q", raw)
		}
		spec.ListenPort = port
	}

	for i, peer := range cfg.Peers {
		peerSpec := PeerSpec{
			PublicKey:    peer.PublicKey,
			PresharedKey: peer.PresharedKey,
			Endpoint:     strings.TrimSpace(peer.Endpoint),
		}
		for _, allowed := range peer.AllowedIPs {
			normalized, err := normalizeAddress(allowed)
			if err != nil {
				return nil, fmt.Errorf("peer %d: invalid AllowedIPs entry %q: %v", i+1, allowed, err)
			}
			peerSpec.AllowedIPs = append(peerSpec.AllowedIPs, normalized)
		}
		if raw := strings.TrimSpace(peer.PersistentKeepalive); raw != "" {
			keepalive, err := strconv.Atoi(raw)
			if err != nil || keepalive < 0 || keepalive > 65535 {
				return nil, fmt.Errorf("peer %d: invalid PersistentKeepalive %q", i+1, raw)
			}
			peerSpec.PersistentKeepalive = keepalive
		}
		spec.Peers = append(spec.Peers, peerSpec)
	}
	if len(spec.Peers) == 0 {
		return nil, fmt.Errorf("profile %q has no peers", profile.Name)
	}
	return spec, nil
}

// normalizeAddress validates an address or CIDR and returns CIDR notation
// (bare addresses become /32 or /128, matching wg-quick behavior).
func normalizeAddress(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty address")
	}
	if strings.Contains(trimmed, "/") {
		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			return "", err
		}
		return prefix.String(), nil
	}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return "", err
	}
	bits := 32
	if addr.Is6() {
		bits = 128
	}
	return netip.PrefixFrom(addr, bits).String(), nil
}

func firstExtra(extras map[string][]string, key string) string {
	if extras == nil {
		return ""
	}
	values := extras[key]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// ResolveEndpoint resolves a host:port endpoint to an IP:port, retrying with
// backoff until the context is cancelled. IPv4 results are preferred.
func ResolveEndpoint(ctx context.Context, endpoint string, logf func(format string, args ...any)) (netip.AddrPort, error) {
	if addrPort, err := netip.ParseAddrPort(endpoint); err == nil {
		return addrPort, nil
	}
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid endpoint %q: %v", endpoint, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return netip.AddrPort{}, fmt.Errorf("invalid endpoint port %q", portStr)
	}

	backoff := time.Second
	for {
		addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err == nil && len(addrs) > 0 {
			chosen := addrs[0]
			for _, addr := range addrs {
				if addr.Is4() || addr.Is4In6() {
					chosen = addr
					break
				}
			}
			return netip.AddrPortFrom(chosen.Unmap(), uint16(port)), nil
		}
		if logf != nil {
			logf("failed to resolve endpoint %s: %v; retrying in %s", host, err, backoff)
		}
		select {
		case <-ctx.Done():
			return netip.AddrPort{}, fmt.Errorf("endpoint resolution cancelled for %q: %w", endpoint, ctx.Err())
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
