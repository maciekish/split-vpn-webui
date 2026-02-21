package routing

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

const resolverCloudflareDoHURL = "https://cloudflare-dns.com/dns-query"

type dohDomainResolver struct {
	baseURL string
	client  *http.Client
}

type dohAnswer struct {
	Type int    `json:"type"`
	Data string `json:"data"`
}

type dohPayload struct {
	Status int         `json:"Status"`
	Answer []dohAnswer `json:"Answer"`
}

func newDoHDomainResolver(timeout time.Duration) *dohDomainResolver {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &dohDomainResolver{
		baseURL: resolverCloudflareDoHURL,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *dohDomainResolver) Resolve(ctx context.Context, domain string) (ResolverValues, error) {
	root := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "*.")
	root = strings.TrimSuffix(root, ".")
	if root == "" {
		return ResolverValues{}, fmt.Errorf("domain is required")
	}

	targets := map[string]struct{}{root: {}}
	cnames, err := r.query(ctx, root, "CNAME", 5)
	if err == nil {
		for _, target := range cnames {
			targets[target] = struct{}{}
		}
	}

	v4Set := make(map[string]struct{})
	v6Set := make(map[string]struct{})
	for target := range targets {
		recordsV4, err := r.query(ctx, target, "A", 1)
		if err == nil {
			for _, value := range recordsV4 {
				v4Set[value+"/32"] = struct{}{}
			}
		}
		recordsV6, err := r.query(ctx, target, "AAAA", 28)
		if err == nil {
			for _, value := range recordsV6 {
				v6Set[value+"/128"] = struct{}{}
			}
		}
	}
	return ResolverValues{
		V4: mapKeysSorted(v4Set),
		V6: mapKeysSorted(v6Set),
	}, nil
}

func (r *dohDomainResolver) query(ctx context.Context, domain, qType string, wantType int) ([]string, error) {
	base, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, err
	}
	query := base.Query()
	query.Set("name", domain)
	query.Set("type", qType)
	base.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/dns-json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("doh status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload dohPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != 0 && payload.Status != 3 {
		return nil, fmt.Errorf("doh response status %d", payload.Status)
	}

	unique := make(map[string]struct{}, len(payload.Answer))
	for _, answer := range payload.Answer {
		if answer.Type != wantType {
			continue
		}
		value := strings.TrimSpace(answer.Data)
		if value == "" {
			continue
		}
		switch wantType {
		case 1:
			ip := net.ParseIP(value)
			if ip == nil || ip.To4() == nil {
				continue
			}
			unique[ip.To4().String()] = struct{}{}
		case 28:
			ip := net.ParseIP(value)
			if ip == nil || ip.To4() != nil {
				continue
			}
			unique[ip.String()] = struct{}{}
		case 5:
			trimmed := strings.TrimSuffix(strings.ToLower(value), ".")
			trimmed = strings.TrimPrefix(trimmed, "*.")
			if trimmed != "" {
				unique[trimmed] = struct{}{}
			}
		}
	}

	values := make([]string, 0, len(unique))
	for value := range unique {
		values = append(values, value)
	}
	sort.Strings(values)
	return values, nil
}
