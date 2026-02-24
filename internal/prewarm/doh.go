package prewarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	defaultDoHTimeout = 10 * time.Second
	cloudflareDoHURL  = "https://cloudflare-dns.com/dns-query"
	googleDoHURL      = "https://dns.google/resolve"
)

// DoHClient resolves DNS records over HTTPS via a specific interface.
type DoHClient interface {
	QueryA(ctx context.Context, domain, iface string) ([]string, error)
	QueryAAAA(ctx context.Context, domain, iface string) ([]string, error)
	QueryCNAME(ctx context.Context, domain, iface string) ([]string, error)
}

// CloudflareDoHClient is an interface-bound DoH client.
type CloudflareDoHClient struct {
	baseURL    string
	timeout    time.Duration
	extraQuery map[string]string
}

type dohAnswer struct {
	Type int    `json:"type"`
	Data string `json:"data"`
}

type dohResponse struct {
	Status int         `json:"Status"`
	Answer []dohAnswer `json:"Answer"`
}

// NewCloudflareDoHClient returns a DoH client pointed at Cloudflare.
func NewCloudflareDoHClient(timeout time.Duration) *CloudflareDoHClient {
	return newDoHClient(cloudflareDoHURL, timeout, nil)
}

// NewCloudflareDoHClientWithURL allows tests to use a custom DoH endpoint.
func NewCloudflareDoHClientWithURL(baseURL string, timeout time.Duration) *CloudflareDoHClient {
	return newDoHClient(baseURL, timeout, nil)
}

// NewGoogleDoHClientWithECS returns an ECS-enabled DoH client using Google Public DNS.
func NewGoogleDoHClientWithECS(timeout time.Duration, ecsSubnet string) *CloudflareDoHClient {
	return newGoogleDoHClientWithURL(googleDoHURL, timeout, ecsSubnet)
}

func newGoogleDoHClientWithURL(baseURL string, timeout time.Duration, ecsSubnet string) *CloudflareDoHClient {
	extra := map[string]string{}
	if trimmed := strings.TrimSpace(ecsSubnet); trimmed != "" {
		extra["edns_client_subnet"] = trimmed
	}
	return newDoHClient(baseURL, timeout, extra)
}

func newDoHClient(baseURL string, timeout time.Duration, extraQuery map[string]string) *CloudflareDoHClient {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = cloudflareDoHURL
	}
	if timeout <= 0 {
		timeout = defaultDoHTimeout
	}
	copiedQuery := make(map[string]string, len(extraQuery))
	for key, value := range extraQuery {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		copiedQuery[k] = v
	}
	return &CloudflareDoHClient{baseURL: trimmed, timeout: timeout, extraQuery: copiedQuery}
}

func (c *CloudflareDoHClient) QueryA(ctx context.Context, domain, iface string) ([]string, error) {
	return c.query(ctx, domain, "A", iface, 1)
}

func (c *CloudflareDoHClient) QueryAAAA(ctx context.Context, domain, iface string) ([]string, error) {
	return c.query(ctx, domain, "AAAA", iface, 28)
}

func (c *CloudflareDoHClient) QueryCNAME(ctx context.Context, domain, iface string) ([]string, error) {
	return c.query(ctx, domain, "CNAME", iface, 5)
}

func (c *CloudflareDoHClient) query(ctx context.Context, domain, qtype, iface string, wantType int) ([]string, error) {
	name := normalizeDomain(domain)
	if name == "" {
		return nil, fmt.Errorf("domain is required")
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}
	query := base.Query()
	for key, value := range c.extraQuery {
		query.Set(key, value)
	}
	query.Set("name", name)
	query.Set("type", qtype)
	base.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/dns-json")

	resp, err := c.httpClient(iface).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("doh status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload dohResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	// 0 = NOERROR, 3 = NXDOMAIN (treated as empty answer set).
	if payload.Status != 0 && payload.Status != 3 {
		return nil, fmt.Errorf("doh response status %d", payload.Status)
	}

	unique := make(map[string]struct{}, len(payload.Answer))
	for _, answer := range payload.Answer {
		if answer.Type != wantType {
			continue
		}
		data := strings.TrimSpace(answer.Data)
		if data == "" {
			continue
		}
		switch wantType {
		case 1:
			ip := net.ParseIP(data)
			if ip == nil || ip.To4() == nil {
				continue
			}
			unique[ip.String()] = struct{}{}
		case 28:
			ip := net.ParseIP(data)
			if ip == nil || ip.To4() != nil {
				continue
			}
			unique[ip.String()] = struct{}{}
		case 5:
			target := normalizeDomain(data)
			if target == "" {
				continue
			}
			unique[target] = struct{}{}
		}
	}

	values := make([]string, 0, len(unique))
	for value := range unique {
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}

func (c *CloudflareDoHClient) httpClient(iface string) *http.Client {
	dialer := &net.Dialer{Timeout: c.timeout}
	if control := interfaceBindControl(iface); control != nil {
		dialer.Control = control
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   c.timeout,
		ResponseHeaderTimeout: c.timeout,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Timeout:   c.timeout,
		Transport: transport,
	}
}

func normalizeDomain(domain string) string {
	trimmed := strings.ToLower(strings.TrimSpace(domain))
	trimmed = strings.TrimPrefix(trimmed, "*.")
	trimmed = strings.TrimSuffix(trimmed, ".")
	return trimmed
}
