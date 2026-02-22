package version

import (
	"strings"
	"testing"
)

func TestCurrentUsesBuildVars(t *testing.T) {
	origVersion := AppVersion
	origCommit := GitCommit
	origBuild := BuildTime
	t.Cleanup(func() {
		AppVersion = origVersion
		GitCommit = origCommit
		BuildTime = origBuild
	})

	AppVersion = "v1.2.3"
	GitCommit = "abc1234"
	BuildTime = "2026-02-22T12:00:00Z"

	info := Current()
	if info.Version != "v1.2.3" || info.Commit != "abc1234" || info.BuildTime != "2026-02-22T12:00:00Z" {
		t.Fatalf("unexpected metadata: %+v", info)
	}
}

func TestInfoStringIncludesAllFields(t *testing.T) {
	text := Info{
		Version:   "v1.2.3",
		Commit:    "abc1234",
		BuildTime: "2026-02-22T12:00:00Z",
	}.String()
	for _, token := range []string{"split-vpn-webui", "v1.2.3", "abc1234", "2026-02-22T12:00:00Z"} {
		if !strings.Contains(text, token) {
			t.Fatalf("expected %q in %q", token, text)
		}
	}
}
