package prewarm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const dnsPort = "53"

// NameserverClient resolves DNS records against a specific recursive nameserver.
type NameserverClient struct {
	serverAddr string
	timeout    time.Duration
}

// NewNameserverClient creates a nameserver-bound DNS client.
func NewNameserverClient(serverIP string, timeout time.Duration) (*NameserverClient, error) {
	ip := net.ParseIP(strings.TrimSpace(serverIP))
	if ip == nil {
		return nil, fmt.Errorf("invalid nameserver IP: %q", serverIP)
	}
	if timeout <= 0 {
		timeout = defaultDoHTimeout
	}
	return &NameserverClient{
		serverAddr: net.JoinHostPort(ip.String(), dnsPort),
		timeout:    timeout,
	}, nil
}

func (c *NameserverClient) QueryA(ctx context.Context, domain, iface string) ([]string, error) {
	return c.lookupIPs(ctx, domain, iface, "ip4")
}

func (c *NameserverClient) QueryAAAA(ctx context.Context, domain, iface string) ([]string, error) {
	return c.lookupIPs(ctx, domain, iface, "ip6")
}

func (c *NameserverClient) QueryCNAME(ctx context.Context, domain, iface string) ([]string, error) {
	name := normalizeDomain(domain)
	if name == "" {
		return nil, fmt.Errorf("domain is required")
	}

	value, err := c.resolver(iface).LookupCNAME(ctx, fqdn(name))
	if err != nil {
		if isNotFoundDNS(err) {
			return nil, nil
		}
		return nil, err
	}
	target := normalizeDomain(value)
	if target == "" || target == name {
		return nil, nil
	}
	return []string{target}, nil
}

func (c *NameserverClient) lookupIPs(ctx context.Context, domain, iface, family string) ([]string, error) {
	name := normalizeDomain(domain)
	if name == "" {
		return nil, fmt.Errorf("domain is required")
	}

	values, err := c.resolver(iface).LookupNetIP(ctx, family, fqdn(name))
	if err != nil {
		if isNotFoundDNS(err) {
			return nil, nil
		}
		return nil, err
	}

	unique := make(map[string]struct{}, len(values))
	for _, addr := range values {
		ip := addr.AsSlice()
		parsed := net.IP(ip)
		if family == "ip4" {
			if parsed.To4() == nil {
				continue
			}
		} else if parsed.To4() != nil {
			continue
		}
		unique[parsed.String()] = struct{}{}
	}

	out := make([]string, 0, len(unique))
	for value := range unique {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func (c *NameserverClient) resolver(iface string) *net.Resolver {
	dialer := &net.Dialer{Timeout: c.timeout}
	if control := interfaceBindControl(iface); control != nil {
		dialer.Control = control
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, c.serverAddr)
		},
	}
}

func fqdn(domain string) string {
	trimmed := strings.TrimSpace(domain)
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, ".") {
		return trimmed
	}
	return trimmed + "."
}

func isNotFoundDNS(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return true
	}
	return false
}
