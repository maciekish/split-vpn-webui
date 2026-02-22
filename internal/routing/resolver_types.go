package routing

import (
	"context"
	"errors"
)

var (
	// ErrResolverRunInProgress indicates one resolver run is already active.
	ErrResolverRunInProgress = errors.New("resolver run already in progress")
)

// DomainResolver resolves one domain to IPv4/IPv6 prefixes.
type DomainResolver interface {
	Resolve(ctx context.Context, domain string) (ResolverValues, error)
}

// ASNResolver resolves one ASN to IPv4/IPv6 prefixes.
type ASNResolver interface {
	Resolve(ctx context.Context, asn string) (ResolverValues, error)
}

// WildcardResolver discovers known subdomains for one wildcard selector.
type WildcardResolver interface {
	Resolve(ctx context.Context, wildcard string) ([]string, error)
}

// ResolverProviderProgress tracks selector and prefix progress for one provider.
type ResolverProviderProgress struct {
	SelectorsTotal   int `json:"selectorsTotal"`
	SelectorsDone    int `json:"selectorsDone"`
	PrefixesResolved int `json:"prefixesResolved"`
}

// ResolverProgress is the live status emitted while resolver runs.
type ResolverProgress struct {
	StartedAt        int64                               `json:"startedAt"`
	SelectorsTotal   int                                 `json:"selectorsTotal"`
	SelectorsDone    int                                 `json:"selectorsDone"`
	PrefixesResolved int                                 `json:"prefixesResolved"`
	CurrentSelector  string                              `json:"currentSelector,omitempty"`
	PerProvider      map[string]ResolverProviderProgress `json:"perProvider,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine publication.
func (p ResolverProgress) Clone() ResolverProgress {
	cloned := ResolverProgress{
		StartedAt:        p.StartedAt,
		SelectorsTotal:   p.SelectorsTotal,
		SelectorsDone:    p.SelectorsDone,
		PrefixesResolved: p.PrefixesResolved,
		CurrentSelector:  p.CurrentSelector,
	}
	if len(p.PerProvider) > 0 {
		cloned.PerProvider = make(map[string]ResolverProviderProgress, len(p.PerProvider))
		for key, value := range p.PerProvider {
			cloned.PerProvider[key] = value
		}
	}
	return cloned
}

// ResolverStatus is returned by resolver status endpoints.
type ResolverStatus struct {
	Running  bool               `json:"running"`
	LastRun  *ResolverRunRecord `json:"lastRun,omitempty"`
	Progress *ResolverProgress  `json:"progress,omitempty"`
}

type resolverStats struct {
	SelectorsTotal   int
	SelectorsDone    int
	PrefixesResolved int
	PerProvider      map[string]ResolverProviderProgress
}

type runResolvers struct {
	domain   DomainResolver
	asn      ASNResolver
	wildcard WildcardResolver
}

func cloneResolverProviderProgress(raw map[string]ResolverProviderProgress) map[string]ResolverProviderProgress {
	if len(raw) == 0 {
		return nil
	}
	cloned := make(map[string]ResolverProviderProgress, len(raw))
	for key, value := range raw {
		cloned[key] = value
	}
	return cloned
}
