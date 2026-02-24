package prewarm

import (
	"fmt"
	"strings"
)

func buildQueryResolvers(primary DoHClient, opts WorkerOptions) ([]DoHClient, error) {
	// Cloudflare DoH (primary) always remains first and is always queried.
	resolvers := []DoHClient{primary}
	seen := map[string]struct{}{"cloudflare-default": {}}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDoHTimeout
	}

	for _, resolver := range opts.AdditionalResolvers {
		if resolver == nil {
			continue
		}
		key := fmt.Sprintf("additional-%T-%p", resolver, resolver)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		resolvers = append(resolvers, resolver)
	}

	for _, nameserver := range opts.ExtraNameservers {
		normalized := strings.TrimSpace(nameserver)
		if normalized == "" {
			continue
		}
		key := "ns:" + normalized
		if _, exists := seen[key]; exists {
			continue
		}
		client, err := NewNameserverClient(normalized, timeout)
		if err != nil {
			return nil, err
		}
		seen[key] = struct{}{}
		resolvers = append(resolvers, client)
	}

	for _, profile := range opts.ECSProfiles {
		subnet := strings.TrimSpace(profile)
		if subnet == "" {
			continue
		}
		key := "ecs:" + subnet
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		resolvers = append(resolvers, NewGoogleDoHClientWithECS(timeout, subnet))
	}
	return resolvers, nil
}
