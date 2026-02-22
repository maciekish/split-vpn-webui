package update

import (
	"time"

	"split-vpn-webui/internal/version"
)

// ReleaseAsset describes one downloadable artifact in a GitHub release.
type ReleaseAsset struct {
	Name string
	URL  string
	Size int64
}

// ReleaseMetadata contains the subset of GitHub release metadata needed by the updater.
type ReleaseMetadata struct {
	Tag         string
	Name        string
	PublishedAt time.Time
	Prerelease  bool
	Assets      []ReleaseAsset
}

// Status captures updater state exposed by the API.
type Status struct {
	Current              version.Info `json:"current"`
	LatestVersion        string       `json:"latestVersion,omitempty"`
	LatestPublishedAt    *time.Time   `json:"latestPublishedAt,omitempty"`
	UpdateAvailable      bool         `json:"updateAvailable"`
	InProgress           bool         `json:"inProgress"`
	State                string       `json:"state"`
	Message              string       `json:"message,omitempty"`
	LastError            string       `json:"lastError,omitempty"`
	LastCheckedAt        *time.Time   `json:"lastCheckedAt,omitempty"`
	LastAttemptedVersion string       `json:"lastAttemptedVersion,omitempty"`
	LastAttemptAt        *time.Time   `json:"lastAttemptAt,omitempty"`
	LastSuccessVersion   string       `json:"lastSuccessVersion,omitempty"`
	LastSuccessAt        *time.Time   `json:"lastSuccessAt,omitempty"`
}

type persistedStatus struct {
	LatestVersion        string `json:"latestVersion,omitempty"`
	LatestPublishedAt    int64  `json:"latestPublishedAt,omitempty"`
	InProgress           bool   `json:"inProgress"`
	State                string `json:"state,omitempty"`
	Message              string `json:"message,omitempty"`
	LastError            string `json:"lastError,omitempty"`
	LastCheckedAt        int64  `json:"lastCheckedAt,omitempty"`
	LastAttemptedVersion string `json:"lastAttemptedVersion,omitempty"`
	LastAttemptAt        int64  `json:"lastAttemptAt,omitempty"`
	LastSuccessVersion   string `json:"lastSuccessVersion,omitempty"`
	LastSuccessAt        int64  `json:"lastSuccessAt,omitempty"`
}

// Job describes a prepared update staging artifact consumed by --self-update-run.
type Job struct {
	TargetVersion  string `json:"targetVersion"`
	AssetName      string `json:"assetName"`
	StagedBinary   string `json:"stagedBinary"`
	ExpectedSHA256 string `json:"expectedSha256"`
	PreparedAt     int64  `json:"preparedAt"`
	ReleaseSource  string `json:"releaseSource,omitempty"`
}

// Options configures the updater manager.
type Options struct {
	Repo        string
	DataDir     string
	BinaryPath  string
	ServiceName string
	UpdaterUnit string
	HTTPClient  HTTPDoer
	Systemd     UnitController
}

// UnitController is the minimal systemd surface required by the updater manager.
type UnitController interface {
	WriteUnit(unitName, content string) error
	Start(unitName string) error
}
