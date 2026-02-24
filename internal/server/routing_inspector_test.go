package server

import "testing"

func TestCanonicalizeSetValue(t *testing.T) {
	cases := []struct {
		name   string
		value  string
		family string
		want   string
	}{
		{name: "ipv4 host", value: "1.1.1.1", family: "inet", want: "1.1.1.1/32"},
		{name: "ipv4 cidr", value: "1.1.1.0/24", family: "inet", want: "1.1.1.0/24"},
		{name: "ipv6 host", value: "2606:4700::1", family: "inet6", want: "2606:4700::1/128"},
		{name: "ipv6 cidr", value: "2606:4700::/32", family: "inet6", want: "2606:4700::/32"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalizeSetValue(tc.value, tc.family); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBuildRoutingInspectorSetIncludesProvenanceAndDevice(t *testing.T) {
	provenance := map[string]map[string]struct{}{
		"192.168.1.20/32": {"source CIDR: 192.168.1.20/32": {}},
	}
	directory := deviceDirectory{
		byMAC:  map[string]string{},
		byIP:   map[string]string{"192.168.1.20": "Media Player"},
		ipsMAC: map[string]map[string]struct{}{},
	}
	set := buildRoutingInspectorSet(
		"svpn_lan_r1s4",
		"inet",
		ipsetSnapshot{Count: 1, Members: []string{"192.168.1.20"}},
		[]string{"192.168.1.20"},
		provenance,
		directory,
		true,
	)
	if set.Name != "svpn_lan_r1s4" {
		t.Fatalf("unexpected set name: %q", set.Name)
	}
	if set.EntryCount != 1 || len(set.Entries) != 1 {
		t.Fatalf("expected one entry, got count=%d len=%d", set.EntryCount, len(set.Entries))
	}
	entry := set.Entries[0]
	if entry.DeviceName != "Media Player" {
		t.Fatalf("expected device name, got %q", entry.DeviceName)
	}
	if len(entry.Provenance) != 1 || entry.Provenance[0] != "source CIDR: 192.168.1.20/32" {
		t.Fatalf("unexpected provenance: %#v", entry.Provenance)
	}
}

func TestBuildRoutingInspectorSetUsesRawMembersWhenProvided(t *testing.T) {
	provenance := map[string]map[string]struct{}{
		"198.51.100.10/32": {"domain api.contoso.com (resolver)": {}},
	}
	set := buildRoutingInspectorSet(
		"svpn_web_r1d4",
		"inet",
		ipsetSnapshot{Count: 1, Members: []string{"198.51.100.10/31"}},
		[]string{"198.51.100.10/32"},
		provenance,
		deviceDirectory{},
		false,
	)
	if set.EntryCount != 1 {
		t.Fatalf("expected runtime entry count to stay 1, got %d", set.EntryCount)
	}
	if len(set.Entries) != 1 {
		t.Fatalf("expected one displayed entry, got %d", len(set.Entries))
	}
	if set.Entries[0].Value != "198.51.100.10/32" {
		t.Fatalf("expected raw member display, got %q", set.Entries[0].Value)
	}
}

func TestNormalizeASNSelector(t *testing.T) {
	if got := normalizeASNSelector("as001335"); got != "AS1335" {
		t.Fatalf("expected AS1335, got %q", got)
	}
	if got := normalizeASNSelector("garbage"); got != "" {
		t.Fatalf("expected empty asn for garbage input, got %q", got)
	}
}
