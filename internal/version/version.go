package version

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Build-time metadata injected via -ldflags.
// Defaults are used for local/dev builds.
var (
	AppVersion = "dev"
	GitCommit  = "unknown"
	BuildTime  = "unknown"
)

// Info describes the running binary build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// Current returns the build metadata for this binary.
func Current() Info {
	return Info{
		Version:   strings.TrimSpace(AppVersion),
		Commit:    strings.TrimSpace(GitCommit),
		BuildTime: strings.TrimSpace(BuildTime),
	}
}

// String returns a human-readable version string.
func (i Info) String() string {
	version := strings.TrimSpace(i.Version)
	if version == "" {
		version = "dev"
	}
	commit := strings.TrimSpace(i.Commit)
	if commit == "" {
		commit = "unknown"
	}
	buildTime := strings.TrimSpace(i.BuildTime)
	if buildTime == "" {
		buildTime = "unknown"
	}
	return fmt.Sprintf("split-vpn-webui %s (commit %s, built %s)", version, commit, buildTime)
}

// JSON returns the metadata encoded as JSON.
func (i Info) JSON() ([]byte, error) {
	return json.Marshal(i)
}
