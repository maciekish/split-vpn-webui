package prewarm

// VPNProgress tracks run progress for a specific VPN interface.
type VPNProgress struct {
	Interface        string `json:"interface"`
	TotalDomains     int    `json:"totalDomains"`
	DomainsProcessed int    `json:"domainsProcessed"`
	IPsInserted      int    `json:"ipsInserted"`
	Errors           int    `json:"errors"`
}

// Progress is emitted during live pre-warm runs.
type Progress struct {
	StartedAt        int64                  `json:"startedAt"`
	TotalDomains     int                    `json:"totalDomains"`
	ProcessedDomains int                    `json:"processedDomains"`
	TotalIPs         int                    `json:"totalIps"`
	PerVPN           map[string]VPNProgress `json:"perVpn"`
}

// CachedSetValues stores discovered IPv4/IPv6 destinations for one ipset.
type CachedSetValues struct {
	V4 []string
	V6 []string
}

// Clone returns a deep copy safe for cross-goroutine publication.
func (p Progress) Clone() Progress {
	cloned := Progress{
		StartedAt:        p.StartedAt,
		TotalDomains:     p.TotalDomains,
		ProcessedDomains: p.ProcessedDomains,
		TotalIPs:         p.TotalIPs,
		PerVPN:           make(map[string]VPNProgress, len(p.PerVPN)),
	}
	for key, value := range p.PerVPN {
		cloned.PerVPN[key] = value
	}
	return cloned
}

// RunStats summarises one completed run.
type RunStats struct {
	DomainsTotal  int
	DomainsDone   int
	IPsInserted   int
	Progress      Progress
	CacheSnapshot map[string]CachedSetValues
}
