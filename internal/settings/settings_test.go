package settings

import (
	"path/filepath"
	"testing"
)

func TestManagerGetMissingReturnsDefaults(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), "settings.json"))
	current, err := manager.Get()
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if current.ListenInterface != "" || current.WANInterface != "" {
		t.Fatalf("expected empty defaults, got %+v", current)
	}
}

func TestManagerSaveAndGetRoundTrip(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), "settings.json"))
	domainEnabled := true
	asnEnabled := false
	wildcardEnabled := true
	debugLogEnabled := true
	input := Settings{
		ListenInterface:                "br0",
		WANInterface:                   "eth8",
		PrewarmParallelism:             8,
		PrewarmDoHTimeoutSeconds:       12,
		PrewarmIntervalSeconds:         600,
		PrewarmExtraNameservers:        "1.1.1.1\n9.9.9.9",
		PrewarmECSProfiles:             "eu=31.13.64.0/18\n13.228.0.0/15",
		ResolverParallelism:            4,
		ResolverTimeoutSeconds:         9,
		ResolverIntervalSeconds:        120,
		ResolverDomainTimeoutSeconds:   7,
		ResolverASNTimeoutSeconds:      11,
		ResolverWildcardTimeoutSeconds: 13,
		ResolverDomainEnabled:          &domainEnabled,
		ResolverASNEnabled:             &asnEnabled,
		ResolverWildcardEnabled:        &wildcardEnabled,
		DebugLogEnabled:                &debugLogEnabled,
		DebugLogLevel:                  "debug",
		AuthPasswordHash:               "hash",
		AuthToken:                      "token",
	}
	if err := manager.Save(input); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	output, err := manager.Get()
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if output.ListenInterface != "br0" || output.WANInterface != "eth8" {
		t.Fatalf("unexpected network fields: %+v", output)
	}
	if output.PrewarmExtraNameservers != "1.1.1.1\n9.9.9.9" {
		t.Fatalf("unexpected prewarm nameservers: %q", output.PrewarmExtraNameservers)
	}
	if output.PrewarmECSProfiles != "eu=31.13.64.0/18\n13.228.0.0/15" {
		t.Fatalf("unexpected prewarm ecs profiles: %q", output.PrewarmECSProfiles)
	}
	if output.ResolverDomainEnabled == nil || *output.ResolverDomainEnabled != true {
		t.Fatalf("expected resolverDomainEnabled=true, got %+v", output.ResolverDomainEnabled)
	}
	if output.ResolverASNEnabled == nil || *output.ResolverASNEnabled != false {
		t.Fatalf("expected resolverAsnEnabled=false, got %+v", output.ResolverASNEnabled)
	}
	if output.ResolverWildcardEnabled == nil || *output.ResolverWildcardEnabled != true {
		t.Fatalf("expected resolverWildcardEnabled=true, got %+v", output.ResolverWildcardEnabled)
	}
	if output.DebugLogEnabled == nil || *output.DebugLogEnabled != true || output.DebugLogLevel != "debug" {
		t.Fatalf("unexpected debug log settings: enabled=%+v level=%q", output.DebugLogEnabled, output.DebugLogLevel)
	}
	if output.AuthToken != "token" || output.AuthPasswordHash != "hash" {
		t.Fatalf("unexpected auth fields: %+v", output)
	}
}
