package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"split-vpn-webui/internal/vpn"
)

const resolverWildcardEndpoint = "https://crt.sh/"

type crtSHWildcardResolver struct {
	baseURL string
	client  *http.Client
}

type crtSHEntry struct {
	NameValue string `json:"name_value"`
}

func newCRTSHWildcardResolver(timeout time.Duration) *crtSHWildcardResolver {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &crtSHWildcardResolver{
		baseURL: resolverWildcardEndpoint,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *crtSHWildcardResolver) Resolve(ctx context.Context, wildcard string) ([]string, error) {
	baseDomain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(wildcard)), "*.")
	if baseDomain == "" {
		return nil, fmt.Errorf("wildcard domain is required")
	}
	if err := vpn.ValidateDomain(baseDomain); err != nil {
		return nil, err
	}

	base, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, err
	}
	query := base.Query()
	query.Set("q", "%."+baseDomain)
	query.Set("output", "json")
	base.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("wildcard resolver status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload []crtSHEntry
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(payload))
	discovered := make([]string, 0, len(payload))
	for _, entry := range payload {
		for _, candidate := range strings.Split(entry.NameValue, "\n") {
			domain := strings.ToLower(strings.TrimSpace(candidate))
			domain = strings.TrimSuffix(domain, ".")
			domain = strings.TrimPrefix(domain, "*.")
			if domain == "" {
				continue
			}
			if domain != baseDomain && !strings.HasSuffix(domain, "."+baseDomain) {
				continue
			}
			if err := vpn.ValidateDomain(domain); err != nil {
				continue
			}
			if _, exists := seen[domain]; exists {
				continue
			}
			seen[domain] = struct{}{}
			discovered = append(discovered, domain)
		}
	}
	sort.Strings(discovered)
	return discovered, nil
}
