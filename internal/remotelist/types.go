// Package remotelist fetches published selector lists (CIDR, ASN, domain or
// wildcard domain) from HTTP(S) URLs on a per-list schedule, stores their
// canonical entries, and reports when a list's content actually changed so
// routing state is only reapplied when it has to be.
package remotelist

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"split-vpn-webui/internal/routing"
)

// Supported list kinds, mirroring the routing selector they feed.
const (
	KindCIDR     = routing.RemoteListKindCIDR
	KindASN      = routing.RemoteListKindASN
	KindDomain   = routing.RemoteListKindDomain
	KindWildcard = routing.RemoteListKindWildcard
)

const (
	// DefaultRefreshIntervalSeconds is used when a list does not set its own.
	DefaultRefreshIntervalSeconds = 6 * 60 * 60
	// MinRefreshIntervalSeconds keeps published endpoints from being hammered.
	MinRefreshIntervalSeconds = 300
	// MaxRefreshIntervalSeconds caps the schedule at 30 days.
	MaxRefreshIntervalSeconds = 30 * 24 * 60 * 60

	// MaxBodyBytes bounds one download so a runaway URL cannot exhaust memory.
	MaxBodyBytes = 8 << 20
	// MaxEntries matches the default ipset hash:net capacity; a larger list
	// could not be applied in full anyway.
	MaxEntries = 65536
	// MaxDomainEntries is lower than MaxEntries because every domain becomes a
	// resolver selector and a pre-warm DNS task per active VPN interface.
	MaxDomainEntries = 5000
	// MaxWildcardEntries is lower still: each wildcard additionally triggers
	// certificate-transparency subdomain discovery.
	MaxWildcardEntries = 250
)

// MaxEntriesForKind returns the entry cap that applies to one list kind.
func MaxEntriesForKind(kind string) int {
	switch kind {
	case KindDomain:
		return MaxDomainEntries
	case KindWildcard:
		return MaxWildcardEntries
	default:
		return MaxEntries
	}
}

var (
	// ErrValidation indicates an invalid remote list payload.
	ErrValidation = errors.New("remote list validation failed")
	// ErrNotFound indicates the requested remote list id does not exist.
	ErrNotFound = errors.New("remote list not found")
	// ErrReferenced indicates the list is still used by a routing rule.
	ErrReferenced = errors.New("remote list is still referenced")

	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

// List is one configured remote selector source and its last fetch state.
type List struct {
	ID                     int64  `json:"id"`
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	Kind                   string `json:"kind"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
	Enabled                bool   `json:"enabled"`
	EntryCount             int    `json:"entryCount"`
	SkippedCount           int    `json:"skippedCount"`
	LastFetchAt            int64  `json:"lastFetchAt,omitempty"`
	LastSuccessAt          int64  `json:"lastSuccessAt,omitempty"`
	LastChangedAt          int64  `json:"lastChangedAt,omitempty"`
	LastError              string `json:"lastError,omitempty"`
	CreatedAt              int64  `json:"createdAt"`
	UpdatedAt              int64  `json:"updatedAt"`
}

// UpsertRequest is the editable subset of a remote list.
type UpsertRequest struct {
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	Kind                   string `json:"kind"`
	RefreshIntervalSeconds int    `json:"refreshIntervalSeconds"`
	Enabled                *bool  `json:"enabled,omitempty"`
}

// RefreshResult reports the outcome of one refresh of one list.
type RefreshResult struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Changed    bool   `json:"changed"`
	EntryCount int    `json:"entryCount"`
	Skipped    int    `json:"skipped"`
	Error      string `json:"error,omitempty"`
}

// fetchState is the persisted conditional-request and change-detection state.
type fetchState struct {
	ETag         string
	LastModified string
	ContentHash  string
}

// NormalizeUpsert validates and canonicalises an upsert payload.
func NormalizeUpsert(req UpsertRequest) (UpsertRequest, error) {
	out := UpsertRequest{
		Name:                   strings.TrimSpace(req.Name),
		URL:                    strings.TrimSpace(req.URL),
		Kind:                   strings.ToLower(strings.TrimSpace(req.Kind)),
		RefreshIntervalSeconds: req.RefreshIntervalSeconds,
		Enabled:                req.Enabled,
	}
	if !namePattern.MatchString(out.Name) {
		return UpsertRequest{}, fmt.Errorf("%w: invalid list name %q", ErrValidation, req.Name)
	}
	if !isSupportedKind(out.Kind) {
		return UpsertRequest{}, fmt.Errorf(
			"%w: kind must be one of %s",
			ErrValidation,
			strings.Join(routing.RemoteListKinds(), ", "),
		)
	}
	parsed, err := url.Parse(out.URL)
	if err != nil {
		return UpsertRequest{}, fmt.Errorf("%w: invalid url %q", ErrValidation, req.URL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return UpsertRequest{}, fmt.Errorf("%w: url must use http or https", ErrValidation)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return UpsertRequest{}, fmt.Errorf("%w: url must include a host", ErrValidation)
	}
	if out.RefreshIntervalSeconds <= 0 {
		out.RefreshIntervalSeconds = DefaultRefreshIntervalSeconds
	}
	if out.RefreshIntervalSeconds < MinRefreshIntervalSeconds {
		return UpsertRequest{}, fmt.Errorf(
			"%w: refresh interval must be at least %d seconds",
			ErrValidation,
			MinRefreshIntervalSeconds,
		)
	}
	if out.RefreshIntervalSeconds > MaxRefreshIntervalSeconds {
		return UpsertRequest{}, fmt.Errorf(
			"%w: refresh interval must be at most %d seconds",
			ErrValidation,
			MaxRefreshIntervalSeconds,
		)
	}
	return out, nil
}

func isSupportedKind(kind string) bool {
	for _, supported := range routing.RemoteListKinds() {
		if kind == supported {
			return true
		}
	}
	return false
}
