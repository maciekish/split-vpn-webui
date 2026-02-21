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
	"strconv"
	"strings"
	"time"
)

const resolverASNEndpoint = "https://stat.ripe.net/data/announced-prefixes/data.json"

type ripeASNResolver struct {
	baseURL string
	client  *http.Client
}

type ripeResponse struct {
	Data struct {
		Prefixes []struct {
			Prefix string `json:"prefix"`
		} `json:"prefixes"`
	} `json:"data"`
}

func newRIPEASNResolver(timeout time.Duration) *ripeASNResolver {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ripeASNResolver{
		baseURL: resolverASNEndpoint,
		client:  &http.Client{Timeout: timeout},
	}
}

func (r *ripeASNResolver) Resolve(ctx context.Context, asn string) (ResolverValues, error) {
	normalized := normalizeASNKey(asn)
	if normalized == "" || normalized == "AS" {
		return ResolverValues{}, fmt.Errorf("invalid ASN %q", asn)
	}
	base, err := url.Parse(r.baseURL)
	if err != nil {
		return ResolverValues{}, err
	}
	query := base.Query()
	query.Set("resource", normalized)
	base.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return ResolverValues{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return ResolverValues{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return ResolverValues{}, fmt.Errorf("asn resolver status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload ripeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ResolverValues{}, err
	}
	v4 := make(map[string]struct{})
	v6 := make(map[string]struct{})
	for _, entry := range payload.Data.Prefixes {
		trimmed := strings.TrimSpace(entry.Prefix)
		ip, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			continue
		}
		prefix, bits := network.Mask.Size()
		canonical := network.IP.String() + "/" + strconv.Itoa(prefix)
		if ip.To4() != nil && bits == 32 {
			canonical = network.IP.To4().String() + "/" + strconv.Itoa(prefix)
			v4[canonical] = struct{}{}
			continue
		}
		v6[canonical] = struct{}{}
	}

	v4List := make([]string, 0, len(v4))
	for cidr := range v4 {
		v4List = append(v4List, cidr)
	}
	sort.Strings(v4List)

	v6List := make([]string, 0, len(v6))
	for cidr := range v6 {
		v6List = append(v6List, cidr)
	}
	sort.Strings(v6List)

	return ResolverValues{V4: v4List, V6: v6List}, nil
}
