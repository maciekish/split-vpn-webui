package prewarm

import (
	"strings"
	"testing"
)

func TestParseNameserverLines(t *testing.T) {
	got, err := ParseNameserverLines("1.1.1.1\n# comment\n2606:4700:4700::1111 # Cloudflare\n1.1.1.1")
	if err != nil {
		t.Fatalf("ParseNameserverLines failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 nameservers, got %d (%#v)", len(got), got)
	}
	if got[0] != "1.1.1.1" || got[1] != "2606:4700:4700::1111" {
		t.Fatalf("unexpected nameservers: %#v", got)
	}
}

func TestParseNameserverLinesRejectsInvalid(t *testing.T) {
	_, err := ParseNameserverLines("not-an-ip")
	if err == nil {
		t.Fatalf("expected error for invalid nameserver")
	}
	if !strings.Contains(err.Error(), "invalid extra nameserver") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseECSProfiles(t *testing.T) {
	got, err := ParseECSProfiles("eu=31.13.64.0/18\n13.228.0.0/15 # apac\neu-dup=31.13.64.0/18")
	if err != nil {
		t.Fatalf("ParseECSProfiles failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ecs profiles, got %d (%#v)", len(got), got)
	}
	if got[0].Name != "eu" || got[0].Subnet != "31.13.64.0/18" {
		t.Fatalf("unexpected first profile: %#v", got[0])
	}
	if got[1].Name != "13.228.0.0/15" || got[1].Subnet != "13.228.0.0/15" {
		t.Fatalf("unexpected second profile: %#v", got[1])
	}
}

func TestParseECSProfilesRejectsInvalid(t *testing.T) {
	_, err := ParseECSProfiles("bad=invalid-cidr")
	if err == nil {
		t.Fatalf("expected error for invalid ecs profile")
	}
	if !strings.Contains(err.Error(), "invalid ECS subnet") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeMultilineSetting(t *testing.T) {
	input := "1.1.1.1\r\n9.9.9.9\r\n"
	if got := NormalizeMultilineSetting(input); got != "1.1.1.1\n9.9.9.9" {
		t.Fatalf("unexpected normalized setting: %q", got)
	}
}
